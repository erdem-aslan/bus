package bus

import (
	"github.com/golang/protobuf/proto"
	"net"
	"strconv"
	"time"
)

type PayloadType int

const (
	ProtoBuf PayloadType = iota
	Json
)

// Endpoint interface defines the contract for various endpoints with different socket transports.
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
	// uniqueness, Bus is not one of them. If you don't provide an Id or Id() returns nil or returns different values for each call,
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

	HandlerProvider
}

type HandlerProvider interface {

	// Mandatory MessageHandler implementation
	MessageHandler() MessageHandler

	// Optional ContextHandler implementation
	ContextHandler() ContextHandler
}

// HttpEndpoint interface for http|https transport types
type HttpEndpoint interface {

	// Returns the url of the http(s) endpoint
	ResourceUrl() string

	// post|put|get types supported, although in a RESTful perspective, method type 'get' should not be used
	Method() string

	// Currently Protobuf and Json payload types are supported over http|https endpoints
	PayloadType() PayloadType

	HandlerProvider
}

type listenerShutdown struct {
	l net.Listener
	q chan<- struct{}
}

func resolveAddress(e Endpoint) (string, error) {

	if (e.FQDN() == "" && e.Address() == "") ||
		e.Port() == 0 || e.Transport() == "" {

		return "", BusError_DestInfoMissing
	}

	t := e.Transport()

	if t != "tcp" &&
		t != "udp" &&
		t != "ws" &&
		t != "http" &&
		t != "https" {

		return "", BusError_InvalidTransport
	}

	port := strconv.Itoa(e.Port())

	var address string

	// FQDN has a priority over IP
	if e.FQDN() != "" {

		// resolve the fqdn
		addrs, err := net.LookupHost(e.FQDN())

		if err != nil && e.Address() == "" {
			return "", err
		}

		if len(addrs) == 0 && e.Address() == "" {
			return "", BusError_DestInfoMissing
		}

		address = addrs[0]

	} else {
		address = e.Address()
	}

	return address + ":" + port, nil

}

func evalAddressAndKey(e Endpoint) (string, string, error) {

	address, err := resolveAddress(e)

	if err != nil {
		return "", "", err
	}

	cKey := e.Id() + "-" + address + "-" + e.Transport()

	cLock.RLock()
	defer cLock.RUnlock()
	if contexts[cKey] != nil {
		return "", "", BusError_EndpointAlreadyRegistered
	}

	if e.PrototypeInstance() == nil {
		return "", "", BusError_MissingPrototypeInstance
	}

	if e.MessageHandler() == nil {
		return "", "", BusError_MissingMessageHandler
	}

	return address, cKey, nil

}
