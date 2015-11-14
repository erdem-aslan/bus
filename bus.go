// Bus, acts as a messaging "bus" with complete transporting support for protocol buffer objects.
// It handles the framing, reliability of connections (endpoints), throttling and  ordered delivery of messages.
package bus

import (
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	"log"
	"sync"
	"time"
)

type PromiseState string

const (
	SendScheduled        PromiseState = "SendScheduled"
	Queued                                   = "Queued"
	Cancelled                                = "Cancelled"
	Sent                                     = "Sent"
	Failed_Timeout                           = "Failed_Timeout"
	Failed_Serialization                     = "Failed_Serialization"
	Failed_Transport                         = "Failed_Transport"
)

var (

	// contexts map holds the references of 'live' contexts with keys generated via
	// [endpoint's nickname] + [endpoint's ip] + [endpoint's port] + [endpoints's transport]
	//
	// One way to connect to same endpoint multiple times to provide different handles (nicknames) to individual
	// bus.Dial(...) calls.
	contexts map[string]Context = make(map[string]Context)
	cLock    sync.RWMutex

	// Endpoint Errors
	BusError_DestInfoMissing           = errors.New("Missing fqdn/ip or port")
	BusError_InvalidTransport          = errors.New("Invalid transport")
	BusError_MissingMessageHandler     = errors.New("Missing message handler implementation")
	BusError_MissingPrototypeInstance  = errors.New("Prototype instance is missing")
	BusError_EndpointAlreadyRegistered = errors.New("Endpoint already registered")
)

// Destination interface defines the contract for various endpoints with different transports.
type Endpoint interface {

	// TCP/IP stack address information
	IP() string

	// TCP/IP stack port information
	Port() int

	// Fully qualified domain name information.
	// Implementors may choose to provide Hostname (FQDN) instead of IP, bus will try to resolve the FQDN if provided.
	FQDN() string

	// EndpointId is present for correlation between endpoints and contexts
	// More practical usage of different endpointId is when you need to connect to the same endpoint with same ip/port/transport.
	//
	// Bus differentiates the endpoints by generating keys with;
	//
	//	[endpointId]-[ip:port]-[transport]
	//
	EndpointId() string

	// Transport may be one of "tcp|udp|sctp|http|https"
	Transport() string

	// BufferSize, if provided other than zero, defines the message queue size of the endpoint.
	// Bus would still accept messages if Endpoint is not reachable and/or in reconnecting state until endpoint's
	// buffer is full.
	BufferSize() int

	// ShouldReconnect, if true, also returns the maximum retry count. Zero or negative value for retry count means
	// retrying forever.
	ShouldReconnect() (bool, int)

	// PrototypeInstance should return a zero value of User's Protocol Buffer object.
	PrototypeInstance() proto.Message

	// Mandatory MessageHandler implementation
	MessageHandler() MessageHandler

	// Optional ContextHandler implementation
	ContextHandler() ContextHandler
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

// MessageHandler interface exposes one single method for incoming messages.
type MessageHandler interface {

	// All incoming messages are being passed to MessageHandler via HandleMessage method.
	// Bare in mind, bus will forget about the message once the HandleMessage method execution is finished.
	HandleMessage(ctx Context, m proto.Message)
}

var (
	// Context Errors
	BusError_ContextOpening                = errors.New("Context is opening")
	BusError_ContextClosed                 = errors.New("Context is closed")
	BusError_ContextClosing                = errors.New("Context is closing")
	BusError_ContextReconnectingBufferFull = errors.New("Context is reconnecting, cannot buffer message")
	BusError_ContextReconnecting           = errors.New("Context is reconnecting")
	BusError_ContextDisconnected           = errors.New("Context is disconnected")
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

	// String() is nice to have
	fmt.Stringer

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
}

// Every send method returns a Promise.
type Promise interface {

	// Getter method for PromiseState
	State() PromiseState

	// Useful for canceling scheduled/timed and queued messages.
	// Returns error if message is already cancelled, expired, delivered or failed to be delivered
	Cancel() error
}

// Final delivery report of messages will be done via ReportFunc callback.
// It's safe to assume successful delivery of the message if err is nil.
type ReportFunc func(m proto.Message, err error)

func init() {
	log.SetPrefix("<Bus> ")
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)
}

// Idiomatic entry point for client side communication via Bus.
//
// Details about 'Endpoint', 'Context' and possible error values are documented individually.
func Dial(e Endpoint) (Context, error) {

	cLock.Lock()
	defer cLock.Unlock()

	destAddress, cKey, err := resolveEndpoint(e)

	if err != nil {
		return nil, err
	}

	ctx := &busContext{
		e:            e,
		resolvedDest: destAddress,
		ctxId:        cKey,
	}

	ctx.setState(Opening)

	err = dial(e.Transport(), destAddress, ctx)

	if err != nil {
		return nil, err
	}

	contexts[cKey] = ctx

	return ctx, nil
}
