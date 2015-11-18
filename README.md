# GOBus

GOBus, 'Bus' in short, is a  tiny framework for message-driven development in golang, using protocol buffers objects over the wire. Networking is based on peer to peer client/server architecture.

Bus in a nutshell, acts as a messaging "bus" with complete transporting support for protocol buffer objects.
It handles the framing, reliability of connections (tcp|udp|ws (websocket) endpoints), throttling (all endpoints) and  ordered delivery (all endpoints) of messages.

Although, messaging structure is defined via protobuf, since protobuf supports JSON, serialization and deserialization may be in JSON format
for http|https endpoints if desired.

All exposed messaging methods / functions are, unless explicitly documented, work asynchronously. In Bus, everything is a 'Promise'.

If you are coming from Java or a JVM based language, you may find some similarities with Netty framework but Bus is more lightweight in terms of both resources and functionality, so tiny means tiny.

While, internals are still changing (primarily for increasing readability and performance), declared interfaces and public APIs are locked / final so you can safely start using Bus.
More importantly, if you have a feature idea that extends (or composes ?) the current feature set without breaking the API, feel free to submit a change request or a pull request.


## Interfaces

 ***Contexts :*** Abstraction over endpoints for sending messages over the wire.

 ***Promises :*** Message cancellation and state exposure.

 ***Endpoints :*** Network nodes representing both client and server side interactions which are tcp,udp,ws and http(s)

 ***Handlers :*** Interfaces for event and state reporting asynchronously.

### Endpoint interfaces

Every network node, server or client, is defined via **Endpoint** and **HttpEndpoint** interfaces in Bus. Implementations of these interfaces are your entry to the framework.

***Endpoint***


```
    type Endpoint interface {

 	// Client side;
 	//
 	// Optional, you may choose to return ""
 	//
 	// Id is present for correlation between endpoints and contexts.
 	// More practical usage of different endpointIds is when you need to connect to the same endpoint with same ip/port/transport.
 	//
 	// Bus differentiates the endpoints by generating keys with;
 	//
 	//	[Id]-[ip:port]-[transport] for tcp|udp|ws and [Id]-[ip:port]-[resourceUrl] for http|https endpoints
 	//
 	// Server side;
 	//
 	// Your Endpoint implementation has to return non-"" consistent Id values.
 	//
 	// Usually frameworks/libraries guard themselves by encapsulating their internal logic by not exposing states which represents
 	// uniqueness, Bus is not one of them. If you don't provide an Id or Id() returns "" or returns different values for each call,
 	// your application will surely malfunction.
 	//
 	// So assign an Id and always return the same value for individual endpoint implementations of yours.
 	//
 	// Package level Stop... functions all depend on EndpointId parameter in order to stop serving endpoints.
 	Id() string

 	// Address information, ipv4|ipv6
 	Address() string

 	// Port information
 	Port() int

 	// Fully qualified domain name information.
 	// Implementors may choose to provide Hostname (FQDN) instead of Address, bus will try to resolve the FQDN if provided.
 	FQDN() string

 	// Transport may be one of "tcp|udp|ws"
 	Transport() string

 	// BufferSize, if provided other than zero, defines the message queue size of the endpoint.
 	// Bus would still accept messages if Endpoint is not reachable and/or in reconnecting state until endpoint's
 	// buffer is full.
 	BufferSize() int

 	// PrototypeInstance should return a zero value of User's Protocol Buffer object.
 	PrototypeInstance() proto.Message

 	// Returns 3 parameters;
 	// reconnect true|false, max attempt count between disconnects, delay between attempts.
 	// If you provide zero or negative max attempt count, Bus will try reconnecting forever
 	// This method is used only for client side.
 	ShouldReconnect() (bool, int, time.Duration)

    // Return nil if you don't want any throttling.
    // Check documentation of ThrottlingHandler interface.
    ShouldThrottle () ThrottlingHandler


 	HandlerProvider
 }
```


***HttpEndpoint***

```
type HttpEndpoint interface {

	// Returns the url of the http(s) endpoint
	//
	// ex:  http://localhost/someProtocol/messages
	//      https://localhost:9090/someOtherProtocol/inc/requests
	//
	Url() string

	// post|put|get types supported
	Method() string

	// Currently Protobuf and Json payload types are supported over http|https endpoints
	PayloadType() PayloadType

	HandlerProvider
}

```

Both  interfaces compose another interface which is ***HandlerProvider***.

***HandlerProvider :***

```
type HandlerProvider interface {

	// Mandatory MessageHandler implementation
	MessageHandler() MessageHandler

	// Optional ContextHandler implementation
	ContextHandler() ContextHandler
}

```

MessageHandler interface exposing a single method, is your listening point for incoming messages. Bus will return error if you don't provide one and every HandleMessage callback is executed in a new goroutine

```
type MessageHandler interface {

	// All incoming messages are being passed to MessageHandler via HandleMessage method.
	// Bare in mind, bus will forget about the message once the HandleMessage method execution is finished.
	HandleMessage(ctx Context, m proto.Message)
}

```

ContextHandler interface also exposes a single method, is optional if you don't interested in Context state updates like _Opening_, _Open_ etc.

```
// ContextHandler interface defines the contract for context state transition event(s)
// Every callback runs in a separate goroutine, so if you choose to share it among multiple endpoints,
// it should be thread safe (stateless). Briefly state transition callbacks are not sequential but parallel.
//
// Context state transitions are fast, implementors definitely would find themselves parallel execution
// of, for example, Opening and Opened state transition callbacks.
//
// Note: Not all transport types transition through all states.

type ContextHandler interface {
	// State changed callback method
	ContextStateChanged(ctx Context, s ContextState)
}

```

So far we've seen the objects used for, what we call, the setup scope of Bus. The most important object after setup scope is Context which exposes all kinds of messaging functionality to you;

Remember, as we documented earlier, all methods declared are non-blocking.

```
// Every endpoint has a dedicated Context, depending on the underlying transport, implementation may vary.
//
// Send methods return a Promise which provides a way of cancellation.
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
	// Remaining messages that missed the grace period for sending, will be reported back as delivery failure.
	CloseGracefully(t time.Duration)

	// Returns the endpoint which this context attached to.
	Endpoint() Endpoint

	// String() is nice to have
	fmt.Stringer
}

```

## Framing ##

If you are familiar to streaming based protocols, streaming bytes over the wire needs encapsulation, in other words _Framing_.

Bus handles framing via prepending payload size but with a twist and the twist part is important if you are connecting to other systems written outside of Bus, even written other languages.

There are two options for length based framing;

**_Fixed-sized:_**:

 This option requires constant amount of bytes to be prepended, lets say 4 bytes for regular ints, 8 bytes for int64. If your message's size fits in to 2 bytes, the rest of the bytes are obviously will be wasted over the wire.

**_Dynamic-sized:_**:

 With dynamic sizing, Varints encoding is used for framing. No excess waste is transmitted to network and virtually any size of payload can be streamed over the wire.

While, fixed-size framing is a more portable solution, it has its own cavets, especially when you are streaming messages with relatively small sizes.

Imagine streaming a message over the wire with a size of 10 bytes, which is quite normal for heartbeats / keep alives. With the fixed sizing overhead your packets would be nearly twice the size.

So, for these and some other (mostly opinionated) reasons, Bus uses Dynamic-sized frames.

For further documentation about varints and their encoding;

  https://developers.google.com/protocol-buffers/docs/encoding#varints


## Bus Client ##

So, we've implemented an endpoint and provided the MessageHandler. There are only two Package level functions exposed for connecting remote endpoints;

```
func DialEndpoint(e Endpoint) (Context, error)

func DialHttpEndpoint(e HttpEndpoint) (Context, error)
```

Well, that's it.

These two idiomatic functions will construct appropriate context objects for you and you can start sending messages immediately.

Wait, Sending messages immediately?
But, what if the physical connection is not ready? Or, if it fails to connect?

Bus will buffer your Send... requests according to your Endpoint's BufferSize which you can easily provide in your implementation and when context is in a appropriate state (_Open_), your messages will be delivered in the correct order (_FiFo_). Failed connect attempts are also retried by Bus if you say so, automatically.

So, obviously, if your buffer size is zero, your Send... method calls will block until underlying endpoint is ready for sending.


## Bus Server ##

More idiomatic, just one function is all you need and fairly self-documented;

```
// Idiomatic function for server side communication via Bus
//
// As opposed to Dial, Serve provides the contexts attached to given endpoints within ContextHandler
// 'Opening' callback for the first time, due to the nature of being server side.
//
// Usually, server side coding resides within finite state machines attached to individual messaging contexts which in our case,
// ContextHandler and MessageHandler implementations. Practically, if you really need to implement logic out of these two interfaces,
// just hold on to Context within ContextHandler's 'Opening' callback and use the context wherever you see fit (similar to client side usage).
// Like client side bus usage, ContextHandler implementation is still optional.
//
// TL;DR:
//
// If you need to send a message to connected clients as soon as they arrive, implement the ContextHandler and MessageHandler
//
// If connected clients will start the messaging and depending on the logic you want to provide if you only need to respond to client requests,
// just ignore the ContextHandler implementation, MessageHandler implementation is all you need.
//
// See StopServing and StopServingAll functions for stopping server side endpoints without a context handle.
//
// Serve func never blocks and returns the initial errors, if any, via ResultFunc func callback in a separate goroutine.
// You can pass nil for ResultFunc func parameter if you don't want an error callback.

func Serve(r ResultFunc, e ...Endpoint)

```

## Usage ##

```
go get github.com/gladmir/bus

go test -bench=. -test.v

=== RUN   TestRequest
--- PASS: TestRequest (0.05s)
=== RUN   TestResponse
--- PASS: TestResponse (0.05s)
=== RUN   TestDelayedRequest
--- PASS: TestDelayedRequest (0.10s)
=== RUN   TestDelayedRequestCancel
--- PASS: TestDelayedRequestCancel (0.10s)
PASS
BenchmarkTestTCPEndpoint-8	 1000000	      7581 ns/op
ok  	github.com/gladmir/bus	7.925s

```

If any of the tests fails, check your available network interfaces, sample code and test cases depend on '127.0.0.1' interface.

'Hello word' for networking? Well, here comes the ping request, pong response.

So, lets implement ourselves an endpoint, as long as our implementation satisfies _Endpoint_, we can throw in anything we want (like t *testing.T for testing purposes);

```
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

```

And a MessageHandler for listening Ping requests over the wire;

```
type testMessageHandler struct {
	t      *testing.T
	client bool
}

func (m *testMessageHandler) HandleMessage(ctx Context, msg proto.Message) {

    // ... process the message

    log.Println("Incoming message:", msg, "from ctx:", ctx)

    if msg.(*TestFrame).EventType == TestFrame_PING {
        // lets respond with pong

    	request := &TestFrame{
    		EventType: TestFrame_PONG,
    		Ping: &TestFrame_Ping{
    			uint64(time.Now().Unix()),
    		},
    	}

    	_, err := ctx.Send(request, func(m proto.Message, err error) {
    		if err != nil {
    			log.Println("Message sending failed, due to error:", err)
    		}
    	})

    }



}

```

And a ContextHandler for listening the state changes

```
type testContextHandler struct {
	t      *testing.T
	client bool
}

func (h *testContextHandler) ContextStateChanged(ctx Context, s ContextState) {


    log.Println ("Context state has changed to:", s)

    // we could check the state and execute certain logics accordingly here.
}
```

Let's wrap up and start our Bus server and send a Ping request via DialEndpoint function.

```
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

	bus.Serve(nil, serverEnd)

	var err error

	ctx, err = bus.DialEndpoint(clientEnd)

	if err != nil {
		log.Println(err)
	}

    request := &TestFrame{
        EventType: TestFrame_PING,
        Ping: &TestFrame_Ping{
            uint64(time.Now().Unix()),
        },
    }

    ctx.Send(request, func(msg proto.Message, err error) {
        if err != nil {
            log.Println("Error:", err)
        }
    })
```

Take a look at bus_test.go

# TL;DR #

 1. Provide an implementation of a desired endpoint interface with coupled MessageHandler and/or ContextHandler interfaces
 2. Serve or Dial
 3. Send and Receive messages regardless of endpoint type and serialization format.
 4. Provide some feedback here if anything fails :)

### What's Missing?

1. HttpEndpoint handling is still under development.
2. Websocket support is still under development but since its trivial it will be available, soon.
3. Auto reconnection needs extreme testing for race conditions.
4. Logging is completely absent with one or two exceptions, exposing an optional interface would be nice for logging callbacks or waiting industry to embrace a common logging framework (poor man's choice).
5. Testing is merely present, needs improvement in terms of coverage percentage and functionality.
6. Throttling implementation is ready but needs to be heavily tested internally, but it will be available eventually but not at the moment.


## License ##

The MIT License (MIT)

Copyright (c) 2015 Erdem Aslan

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.