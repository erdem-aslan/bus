package bus

import (
	"fmt"
	"net"
	"time"
	"sync"
	"errors"
	"github.com/golang/protobuf/proto"
)

type ContextState string

const (
	Open          ContextState = "Open"          // Context is in Open state when underlying transport is ready.
	Opening                    = "Opening"       // initial state
	Reopening                  = "Reopening"     // Underlying transport is being reinitialized, depending on endpoint's bufferSize, new messages are allowed to be queued or not.
	UserClosed                 = "UserClosed"    // Closed is two of the final states of Context; user requested shutdown state.
	UserClosing                = "UserClosing"   // User requested shutdown state; no new messages are allowed.
	NetworkClosed              = "NetworkClosed" // NetworkClosed is two of the final states of Context; network/io originated shutdown.
)

var (
	// Context Errors
	BusError_ContextClosed                 = errors.New("Context is closed")
	BusError_ContextOpening                = errors.New("Context is opening")
	BusError_ContextClosing                = errors.New("Context is closing")
	BusError_ContextReconnecting           = errors.New("Context is reconnecting")
	BusError_ContextDisconnected           = errors.New("Context is disconnected")
	BusError_ContextReconnectingBufferFull = errors.New("Context is reconnecting, cannot buffer message")

	// Message Errors
	BusError_IO                  = errors.New("IO/Transport error")
	BusError_AlreadySent         = errors.New("Message already sent")
	BusError_DeliveryFailed      = errors.New("Message delivery has failed")
	BusError_DeliveryTimeout     = errors.New("Message delivery timed out")
	BusError_AlreadyCancelled    = errors.New("Message already cancelled")
	BusError_FailedMarshaling    = errors.New("Message delivery has failed due to marshalling")
	BusError_FailedUnmarshalling = errors.New("Message receiving has failed due to unmarshalling")
)

// Every endpoint has a dedicated Context, depending on the underlying transport, implementation may vary.
//
// Send methods return MessagePromise which provides a way of cancellation.
// Send methods also returns error if Context is closed and/or closing.
//
// All exposed methods accepts a func, ReportFunc, which will be executed at the end of the messages' life cycles.
// Its safe to provide a nil ReportFunc so this parameter is optional
//
type Context interface {

	// Idiomatic Send method, sufficient for most use cases
	Send(m proto.Message, r ReportFunc) (Promise, error)

	// SendAfter method for providing delayed messaging.
	SendAfter(m proto.Message, d time.Duration, r ReportFunc) (Promise, error)

	// SendWithTimeout method for providing validity period of messages.
	// This method's implementations, unless documented specifically, would not create new timers.
	SendWithTimeout(m proto.Message, d time.Duration, r ReportFunc) (Promise, error)

	// SendWithHighPriority method for sending prioritized messages which will bypass other queued messages in terms of ordering
	SendWithHighPriority(m proto.Message, r ReportFunc) (Promise, error)

	// Closes the context and all resources its attached to.
	// Pending messages will be reported back as delivery failure and further message sender methods will return error
	Close()

	// Returns the state if this context.
	State() ContextState

	// Closes gracefully with timeout. Within provided duration, Bus will try to consume all messages while honoring
	// the throttling if configured.
	//
	// Remaining messages that missed the grace period for sending, will be report back as delivery failure.
	CloseGracefully(t time.Duration)

	// Returns the endpoint which this context attached to.
	Endpoint() Endpoint

	// String() is nice to have
	fmt.Stringer

}

// ContextHandler interface defines the contract for context state transition event(s)
// Every callback runs in a separate goroutine, so if implementation is shared among multiple endpoints,
// it should be thread safe (stateless). Briefly state transition callbacks are not sequential.
//
// Context state transitions are fast, implementors definitely would find themselves parallel execution
// of, for example, Opening and Opened state transition callbacks.
//
// Note: Not all transport types transition through all states.
type ContextHandler interface {
	// State changed callback method
	ContextStateChanged(ctx Context, s ContextState)
}

// Context interface implementation, used for socket based transports.
type socketContext struct {

	// contexts map key
	ctxId string
	// resolved dest address for easy reconnection, valid for client contexts
	resolvedDest string
	// reconnection attempt count, valid for client contexts
	rcCount int
	served  bool

	e     Endpoint
	conn  net.Conn
	state ContextState

	// reader/writer shutdown channels
	rs chan struct{}
	ws chan struct{}

	// context send queue
	ctxQueue chan *busPromise

	// user close request chan
	ctxQuit chan struct{}
	// network disconnect signal chan
	netQuit chan struct{}

	sync.RWMutex
}

func (c *socketContext) Send(m proto.Message, r ReportFunc) (Promise, error) {

	err := validateContextState(c)

	if err != nil {
		return nil, err
	}

	b := &busPromise{
		msg:   m,
		rFunc: r,
		state: Queued,
	}

	c.ctxQueue <- b

	return b, nil

}

func (c *socketContext) SendAfter(m proto.Message, d time.Duration, r ReportFunc) (Promise, error) {

	err := validateContextState(c)

	if err != nil {
		return nil, err
	}

	b := &busPromise{
		msg:    m,
		rFunc:  r,
		state:  SendScheduled,
		urgent: true,
	}

	b.expTimer = time.AfterFunc(d, func() {

		if r == nil && c.State() != Open {
			return
		}

		switch c.State() {
		case Opening:
			r(b.msg, BusError_ContextOpening)
		case UserClosing:
			r(b.msg, BusError_ContextClosing)
		case UserClosed:
			r(b.msg, BusError_ContextClosed)
		case Reopening:
			r(b.msg, BusError_ContextReconnecting)
		case NetworkClosed:
			r(b.msg, BusError_ContextDisconnected)
		default:
			c.ctxQueue <- b
		}
	})

	return b, nil
}

func (c *socketContext) SendWithTimeout(m proto.Message, d time.Duration, r ReportFunc) (Promise, error) {

	err := validateContextState(c)

	if err != nil {
		return nil, err
	}

	b := &busPromise{
		msg:        m,
		rFunc:      r,
		state:      Queued,
		validUntil: time.Now().Add(d),
	}

	c.ctxQueue <- b

	return b, nil
}

func (c *socketContext) SendWithHighPriority(m proto.Message, r ReportFunc) (Promise, error) {

	err := validateContextState(c)

	if err != nil {
		return nil, err
	}

	b := &busPromise{
		msg:    m,
		rFunc:  r,
		state:  Queued,
		urgent: true,
	}

	c.ctxQueue <- b

	return b, nil
}

func (c *socketContext) Close() {

	state := c.State()

	if state != Opening && state != Open && state != Reopening {
		return
	}

	c.setState(UserClosed)
	c.ctxQuit <- struct{}{}
}

func (c *socketContext) State() ContextState {
	c.RLock()
	defer c.RUnlock()
	return c.state
}

func (c *socketContext) CloseGracefully(t time.Duration) {

	state := c.State()

	if state != Opening && state != Open && state != Reopening {
		return
	}

	c.setState(UserClosing)
	c.ctxQuit <- struct{}{}
}

func (c *socketContext) Endpoint() Endpoint {
	return c.e
}

func (c *socketContext) String() string {
	return c.ctxId
}

func (c *socketContext) setState(s ContextState) {

	if c.State() == s {
		return
	}

	c.Lock()
	c.state = s
	c.Unlock()

	c.notifyLc(s)

}

func (c *socketContext) notifyLc(s ContextState) {

	l := c.e.ContextHandler()

	if l == nil {
		return
	}

	go l.ContextStateChanged(c, s)

}

// -----------------------------------------------

// internal function to check the state of context
func validateContextState(c *socketContext) error {

	c.RLock()
	defer c.RUnlock()

	switch c.state {
	case UserClosed:
		return BusError_ContextClosed
	case UserClosing:
		return BusError_ContextClosing
	case NetworkClosed:
		return BusError_ContextDisconnected
	case Reopening:
		// len(chan) call is not synchronized so we are estimating a %90 buffer capacity to be full,
		// just to be on the safe side.

		if c.Endpoint().BufferSize() <= 0 {
			// Endpoint does not configured to be buffered
			return BusError_ContextReconnecting

		} else if float64(len(c.ctxQueue)) >= float64(c.Endpoint().BufferSize())*0.9 {

			return BusError_ContextReconnectingBufferFull
		}

	}

	return nil

}
