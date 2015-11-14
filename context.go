package bus

import (
	"github.com/golang/protobuf/proto"
	"log"
	"net"
	"sync"
	"time"
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

// Context interface implementation, currently used for all transport types.
type busContext struct {

	// contexts map key
	ctxId string
	// resolved dest address for easy reconnection
	resolvedDest string
	// reconnection attempt count
	rcCount int

	e     Endpoint
	conn  net.Conn
	state ContextState

	// reader shutdown channel
	rs chan struct{}
	// writer normal queue
	wc chan *busMessagePromise
	// writer priority queue
	pwc chan *busMessagePromise

	sync.RWMutex
}

func (c *busContext) Send(m proto.Message, r ReportFunc) (Promise, error) {

	err := validateContextState(c, false)

	if err != nil {
		return nil, err
	}

	b := &busMessagePromise{
		msg:   m,
		rFunc: r,
		state: Queued,
	}

	c.wc <- b

	return b, nil

}

func (c *busContext) SendAfter(m proto.Message, d time.Duration, r ReportFunc) (Promise, error) {

	err := validateContextState(c, true)

	if err != nil {
		return nil, err
	}

	b := &busMessagePromise{
		msg:   m,
		rFunc: r,
		state: SendScheduled,
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
			c.pwc <- b
		}
	})

	return b, nil
}

func (c *busContext) SendWithTimeout(m proto.Message, d time.Duration, r ReportFunc) (Promise, error) {

	err := validateContextState(c, false)

	if err != nil {
		return nil, err
	}

	b := &busMessagePromise{
		msg:        m,
		rFunc:      r,
		state:      Queued,
		validUntil: time.Now().Add(d),
	}

	c.wc <- b

	return b, nil
}

func (c *busContext) SendWithHighPriority(m proto.Message, r ReportFunc) (Promise, error) {

	err := validateContextState(c, false)

	if err != nil {
		return nil, err
	}

	b := &busMessagePromise{
		msg:   m,
		rFunc: r,
		state: Queued,
	}

	c.pwc <- b

	return b, nil
}

func (c *busContext) Close() {
	c.setState(UserClosed)

	log.Println("Context sending shutdown trigger to reader...")
	c.rs <- struct{}{}

	log.Println("Cleaning up context reference...")
	cLock.Lock()
	defer cLock.Unlock()
	delete(contexts, c.ctxId)

}

func (c *busContext) State() ContextState {
	c.RLock()
	defer c.RUnlock()
	return c.state
}

func (c *busContext) CloseGracefully(t time.Duration) {
	c.setState(UserClosing)
	c.rs <- struct{}{}
}

func (c *busContext) Endpoint() Endpoint {
	return c.e
}

func (c *busContext) String() string {
	return c.ctxId
}

// State setter and ContextHandler callback executor
func (c *busContext) setState(s ContextState) {

	if c.State() == s {
		log.Println("State is already", s, " !")
		return
	}

	c.Lock()
	c.state = s
	log.Println("State updated:", c.state, "for state:", c)
	c.Unlock()

	c.notifyLc(s)

}

func (c *busContext) notifyLc(s ContextState) {

	l := c.e.ContextHandler()

	if l == nil {
		return
	}

	go l.ContextStateChanged(c, s)

}

// -----------------------------------------------

// Promise interface implementation
type busMessagePromise struct {
	state      *PromiseState
	msg        proto.Message
	rFunc      ReportFunc
	expTimer   *time.Timer
	validUntil time.Time

	sync.RWMutex
}

func (p *busMessagePromise) Cancel() error {

	s := p.State()

	switch s {
	case Sent:
		return BusError_AlreadySent
	case Cancelled:
		return BusError_AlreadyCancelled
	case Failed_Serialization:
		return BusError_DeliveryFailed
	case Failed_Transport:
		return BusError_DeliveryFailed
	case Failed_Timeout:
		return BusError_DeliveryTimeout
	}

	p.setState(Cancelled)

	return nil
}

func (p *busMessagePromise) State() PromiseState {
	p.RLock()
	defer p.RUnlock()
	return p.state
}

func (p *busMessagePromise) setState(s PromiseState) {
	p.Lock()
	defer p.Unlock()
	p.state = s
}

// -----------------------------------------------

// internal function to check the state of context
//
// p bool argument: if context state checking being made for prioritized message sending or not
func validateContextState(c *busContext, p bool) error {

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
		}

		if p {
			if float64(len(c.pwc)) >= float64(c.Endpoint().BufferSize())*0.9 {
				return BusError_ContextReconnectingBufferFull
			}
		} else {
			if float64(len(c.wc)) >= float64(c.Endpoint().BufferSize())*0.9 {
				return BusError_ContextReconnectingBufferFull
			}
		}
	}

	return nil

}
