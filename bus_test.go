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

		reply := &TestFrame{
			EventType: TestFrame_PONG,
			Pong: &TestFrame_Pong{
				uint64(time.Now().Unix()),
			},
		}

		ctx.Send(reply, func(msg proto.Message, err error) {
		})
	}
}

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

func TestTCPEndpoint(t *testing.T) {

}

func BenchmarkTestTCPEndpoint(b *testing.B) {

	request := &TestFrame{
		EventType: TestFrame_PING,
		Ping: &TestFrame_Ping{
			uint64(time.Now().Unix()),
		},
	}

	for n := 0; n < b.N; n++ {

		ctx.Send(request, func(msg proto.Message, err error) {
			if err != nil {
				b.Error("Error:", err)
			}
		})

	}

}
