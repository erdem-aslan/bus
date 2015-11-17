package bus

import (
	"github.com/golang/protobuf/proto"
	"sync"
	"time"
)

// Every send method returns a Promise.
type Promise interface {

	// Getter method for PromiseState
	State() PromiseState

	// Useful for canceling scheduled/timed and queued messages.
	// Returns error if message is already cancelled, expired, delivered or failed to be delivered
	Cancel() error
}

// MessageHandler interface exposes one single method for incoming messages.
type MessageHandler interface {

	// All incoming messages are being passed to MessageHandler via HandleMessage method.
	// Bare in mind, bus will forget about the message once the HandleMessage method execution is finished.
	HandleMessage(ctx Context, m proto.Message)
}

// Final delivery report of messages will be done via ReportFunc callback.
// It's safe to assume successful delivery of the message if err is nil.
type ReportFunc func(m proto.Message, err error)

// -----------------------------------------------

// Promise interface implementation
type busPromise struct {
	state      PromiseState
	msg        proto.Message
	rFunc      ReportFunc
	expTimer   *time.Timer
	validUntil time.Time
	urgent     bool

	sync.RWMutex
}

func (p *busPromise) Cancel() error {

	s := p.State()

	switch s {
	case Sent:
		return BusError_AlreadySent
	case Cancelled:
		return BusError_AlreadyCancelled
	case FailedSerialization:
		return BusError_DeliveryFailed
	case FailedTransport:
		return BusError_DeliveryFailed
	case FailedTimeout:
		return BusError_DeliveryTimeout
	}

	p.setState(Cancelled, nil)

	return nil
}

func (p *busPromise) State() PromiseState {
	p.RLock()
	defer p.RUnlock()
	return p.state
}

func (p *busPromise) setState(s PromiseState, err error) {
	p.Lock()
	defer p.Unlock()
	p.state = s

	if p.rFunc != nil {
		go p.rFunc(p.msg, err)
	}

}
