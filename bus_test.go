package bus

import (
	"github.com/golang/protobuf/proto"
	"log"
	"os"
	"testing"
	"time"
)

var (
	clientEnd *TestEndpoint
	serverEnd *TestEndpoint
	ctx       Context
)

func TestMain(m *testing.M) {

	clientEnd = &TestEndpoint{
		id:           "test-server",
		port:         9000,
		address:      "127.0.0.1",
		transport:    "tcp",
		protoMessage: &TestFrame{},
		reconnect:    false,
		bufferSize:   100000,
	}

	clientEnd.contextHandler = &testContextHandler{client: true}
	clientEnd.messageHandler = &testMessageHandler{client: true}

	serverEnd = &TestEndpoint{
		id:           "test-server",
		port:         9000,
		address:      "127.0.0.1",
		transport:    "tcp",
		protoMessage: &TestFrame{},
	}

	serverEnd.contextHandler = &testContextHandler{client: false}
	serverEnd.messageHandler = &testMessageHandler{client: false}

	Serve(nil, serverEnd)

	var err error

	ctx, err = DialEndpoint(clientEnd)

	if err != nil {
		log.Println(err)
		os.Exit(-1)
	}

	os.Exit(m.Run())
}

func TestRequest(t *testing.T) {

	serverEnd.messageHandler.t = t

	request := &TestFrame{
		EventType: TestFrame_PING,
		Ping: &TestFrame_Ping{
			uint64(time.Now().Unix()),
		},
	}

	_, err := ctx.Send(request, func(m proto.Message, err error) {
		if err != nil {
			t.Fail()
		}
	})

	if err != nil {
		t.Fail()
	}

	time.Sleep(time.Millisecond * 500)
}

func TestResponse(t *testing.T) {

	clientEnd.messageHandler.t = t

	request := &TestFrame{
		EventType: TestFrame_PING,
		Ping: &TestFrame_Ping{
			uint64(time.Now().Unix()),
		},
	}

	_, err := ctx.Send(request, func(m proto.Message, err error) {
		if err != nil {
			t.Fail()
		}
	})

	if err != nil {
		t.Fail()
	}

	time.Sleep(time.Millisecond * 50)
}

func TestDelayedRequest(t *testing.T) {
	serverEnd.messageHandler.t = t

	request := &TestFrame{
		EventType: TestFrame_PING,
		Ping: &TestFrame_Ping{
			uint64(time.Now().Unix()),
		},
	}

	promise, err := ctx.SendAfter(request, time.Millisecond*50, func(m proto.Message, err error) {
		if err != nil {
			t.Fail()
		}
	})

	if err != nil {
		t.Fail()
	}

	if promise.State() != SendScheduled {
		t.Fail()
	}

	time.Sleep(time.Millisecond * 100)

	if promise.State() == SendScheduled || promise.State() != Sent {
		t.Fail()
	}

}

func TestDelayedRequestCancel(t *testing.T) {
	serverEnd.messageHandler.t = t

	request := &TestFrame{
		EventType: TestFrame_PING,
		Ping: &TestFrame_Ping{
			uint64(time.Now().Unix()),
		},
	}

	promise, err := ctx.SendAfter(request, time.Millisecond*50, func(m proto.Message, err error) {
		if err != nil {
			t.Fail()
		}
	})

	if err != nil {
		t.Fail()
	}

	promise.Cancel()

	if promise.State() != Cancelled {
		t.Fail()
	}

	time.Sleep(time.Millisecond * 100)

	if promise.State() == Sent {
		t.Fail()
	}

}

func BenchmarkTestTCPEndpoint(b *testing.B) {

	for n := 0; n < b.N; n++ {

		request := &TestFrame{
			EventType: TestFrame_PING,
			Ping: &TestFrame_Ping{
				uint64(time.Now().Unix()),
			},
		}

		ctx.Send(request, func(msg proto.Message, err error) {
			if err != nil {
				b.Error("Error:", err)
			}
		})

	}

}

type TestEndpoint struct {
	id             string
	port           int
	fqdn           string
	address        string
	transport      string
	bufferSize     int
	protoMessage   proto.Message
	reconnect      bool
	maxRecCount    int
	recDelay       time.Duration
	t              *testing.T
	messageHandler *testMessageHandler
	contextHandler *testContextHandler
}

func (e *TestEndpoint) Id() string {
	return e.id
}

func (e *TestEndpoint) Address() string {
	return e.address
}

func (e *TestEndpoint) Port() int {
	return e.port
}

func (e *TestEndpoint) FQDN() string {
	return e.fqdn
}

func (e *TestEndpoint) Transport() string {
	return e.transport
}

func (e *TestEndpoint) BufferSize() int {
	return e.bufferSize
}

func (e *TestEndpoint) PrototypeInstance() proto.Message {
	return e.protoMessage
}

func (e *TestEndpoint) ShouldReconnect() (bool, int, time.Duration) {
	return e.reconnect, e.maxRecCount, e.recDelay
}

func (e *TestEndpoint) ShouldThrottle() ThrottlingHandler {
	return nil
}

func (e *TestEndpoint) MessageHandler() MessageHandler {
	return e.messageHandler
}

func (e *TestEndpoint) ContextHandler() ContextHandler {
	return e.contextHandler
}

type testContextHandler struct {
	t      *testing.T
	client bool
}

func (h *testContextHandler) ContextStateChanged(ctx Context, s ContextState) {

}

type testMessageHandler struct {
	t      *testing.T
	client bool
}

func (m *testMessageHandler) HandleMessage(ctx Context, msg proto.Message) {

	if !m.client {

		switch msg.(type) {
		default:
			if m.t != nil {
				m.t.Fail()
			}
		case *TestFrame:
			if msg.(*TestFrame).EventType != TestFrame_PING {
				if m.t != nil {
					m.t.Fail()
				}
			}

		}

		reply := &TestFrame{
			EventType: TestFrame_PONG,
			Pong: &TestFrame_Pong{
				uint64(time.Now().Unix()),
			},
		}

		_, err := ctx.Send(reply, func(msg proto.Message, err error) {
			if err != nil {
				if m.t != nil {
					m.t.Fail()
				}
			}
		})

		if err != nil {
			if m.t != nil {
				m.t.Fail()
			}
		}
	} else {
		switch msg.(type) {
		default:
			if m.t != nil {
				m.t.Fail()
			}
		case *TestFrame:
			if msg.(*TestFrame).EventType != TestFrame_PONG {
				if m.t != nil {
					m.t.Fail()
				}
			}

		}

	}
}
