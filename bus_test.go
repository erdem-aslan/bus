package bus

import (
	"github.com/golang/protobuf/proto"
	"log"
	"os"
	"testing"
	"time"
)

var (
	clientEnd *Endpoint
	serverEnd *Endpoint
	ctx Context
)

func TestMain(m *testing.M) {

	clientEnd = &Endpoint{
		Id:           "test-server",
		Port:         9000,
		Address:      "127.0.0.1",
		Transport:    "tcp",
		P: &TestFrame{},
		BufferSize:   100000,
	}

	clientEnd.C = &testContextHandler{client: true}
	clientEnd.M = &testMessageHandler{client: true}

	serverEnd = &Endpoint{
		Id:           "test-server",
		Port:         9000,
		Address:      "127.0.0.1",
		Transport:    "tcp",
		P: &TestFrame{},
	}

	serverEnd.C = &testContextHandler{client: false}
	serverEnd.M = &testMessageHandler{client: false}

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

	request := &TestFrame{
		EventType: TestFrame_PING,
		Ping: &TestFrame_Ping{
			uint64(time.Now().Unix()),
		},
	}

	promise, err := ctx.SendAfter(request, time.Millisecond * 50, func(m proto.Message, err error) {
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

	request := &TestFrame{
		EventType: TestFrame_PING,
		Ping: &TestFrame_Ping{
			uint64(time.Now().Unix()),
		},
	}

	promise, err := ctx.SendAfter(request, time.Millisecond * 50, func(m proto.Message, err error) {
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

type testContextHandler struct {
	t      *testing.T
	client bool
}

func (h *testContextHandler) ContextStateChanged(ctx Context, s ContextState) {
	log.Println(ctx,s)
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
