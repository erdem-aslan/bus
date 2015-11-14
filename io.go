package bus

import (
	"bufio"
	"encoding/binary"
	"errors"
	"github.com/golang/protobuf/proto"
	"log"
	"net"
	"strconv"
	"time"
)

var (
	// Message Errors
	BusError_AlreadyCancelled    = errors.New("Message already cancelled")
	BusError_AlreadySent         = errors.New("Message already sent")
	BusError_DeliveryFailed      = errors.New("Message delivery has failed")
	BusError_DeliveryTimeout     = errors.New("Message delivery timed out")
	BusError_FailedMarshaling    = errors.New("Message delivery has failed due to marshalling")
	BusError_FailedUnmarshalling = errors.New("Message receiving has failed due to unmarshalling")
	BusError_IO                  = errors.New("IO/Transport error")
)

func redial(ctx *busContext) error {

	ctx.rcCount++
	ctx.conn.Close()

	log.Println("Reopening context:", ctx)

	err := dial(ctx.e.Transport(), ctx.resolvedDest, ctx)

	if err != nil {
		// reconnection has failed
		ctx.setState(NetworkClosed)

		_, c := ctx.Endpoint().ShouldReconnect()

		if ctx.rcCount < c {
			ctx.setState(Reopening)
			time.AfterFunc(time.Second*5, func() {
				redial(ctx)
			})
		} else {
			log.Println("Given up reopening for context:", ctx)
		}

	}

	return nil

}

func dial(transport string, addr string, ctx *busContext) error {

	conn, err := net.Dial(transport, addr)

	if err != nil {
		return err
	}

	ctx.conn = conn

	writeErrorChan := newWriter(ctx)
	readChan := newReader(ctx)

	if ctx.State() == Reopening {
		ctx.rcCount = 0
	}

	go func(ctx *busContext) {

		var readOpen bool
		var message proto.Message

		defer ctx.conn.Close()

	infinite:
		for {
			select {
			case message, readOpen = <-readChan:

				if !readOpen {
					// close the writer
					close(ctx.wc)
					close(ctx.pwc)

					r, c := ctx.Endpoint().ShouldReconnect()

					if r && ctx.rcCount < c {
						ctx.setState(Reopening)
						// @todo: next attempt delay should be configurable, at Endpoint level
						time.AfterFunc(time.Second*5, func() {
							redial(ctx)
						})
					}

					break infinite
				}

				ctx.e.MessageHandler().HandleMessage(ctx, message)

			case err = <-writeErrorChan:
				ctx.rs <- struct{}{}
				break infinite
			}
		}

	}(ctx)

	ctx.setState(Open)

	return nil

}

func resolveEndpoint(e Endpoint) (string, string, error) {

	destAddress, err := resolveDestination(e)

	if err != nil {
		return "", "", err
	}

	cKey := e.EndpointId() + "-" + destAddress + e.Transport()

	if contexts[cKey] != nil {
		return "", "", BusError_EndpointAlreadyRegistered
	}

	if e.PrototypeInstance() == nil {
		return "", "", BusError_MissingPrototypeInstance
	}

	if e.MessageHandler() == nil {
		// we won't operate without a valid message handler
		return "", "", BusError_MissingMessageHandler
	}

	return destAddress, cKey, nil

}

func resolveDestination(e Endpoint) (string, error) {

	if (e.FQDN() == "" && e.IP() == "") ||
		e.Port() == 0 || e.Transport() == "" {

		return "", BusError_DestInfoMissing
	}

	t := e.Transport()

	if t != "tcp" &&
		t != "udp" &&
		t != "sctp" &&
		t != "http" &&
		t != "https" {

		return "", BusError_InvalidTransport
	}

	port := strconv.Itoa(e.Port())

	var destAddress string

	// FQDN has a priority over IP
	if e.FQDN() != "" {

		// resolve the fqdn
		addrs, err := net.LookupHost(e.FQDN())

		if err != nil && e.IP() == "" {
			return "", err
		}

		if len(addrs) == 0 && e.IP() == "" {
			return "", BusError_DestInfoMissing
		}

		destAddress = addrs[0]

	} else {
		destAddress = e.IP()
	}

	return destAddress + ":" + port, nil

}

func newWriter(ctx *busContext) <-chan error {

	bs := ctx.Endpoint().BufferSize()

	if bs < 0 {
		bs = 0
	}

	wc := make(chan *busMessagePromise, ctx.e.BufferSize())
	pwc := make(chan *busMessagePromise, ctx.e.BufferSize())

	ec := make(chan error)

	writer := bufio.NewWriter(ctx.conn)

	ctx.wc = wc
	ctx.pwc = pwc

	go func(ctx Context,
		w *bufio.Writer, wc chan *busMessagePromise,
		pwc chan *busMessagePromise,
		ec chan<- error) {

		defer log.Println("Writer exiting for context:", ctx)

		b := proto.NewBuffer(make([]byte, 0, 4096))

		var promise *busMessagePromise
		var err error
		var pOk bool
		var ok bool

		for {

			select {
			case promise, pOk = <-pwc:
			case promise, ok = <-wc:
			}

			if !ok && !pOk {
				close(ec)
				return
			}

			if promise == nil || promise.State() == Cancelled {
				continue
			}

			if promise.validUntil.Before(time.Now()) {
				// message expired
				promise.setState(Failed_Timeout)

				if promise.rFunc != nil {
					promise.rFunc(promise.msg, BusError_DeliveryTimeout)
				}

			}

			err = b.EncodeMessage(promise.msg)

			if err != nil {

				promise.setState(Failed_Serialization)

				if promise.rFunc != nil {
					promise.rFunc(promise.msg, BusError_FailedMarshaling)
				}

				ec <- BusError_FailedMarshaling

				b.Reset()

				continue
			}

			w.Write(b.Bytes())
			err = w.Flush()

			// write error is fatal, remote endpoint may try to decode malformed bytes.
			// BusError_IO will trigger a reconnection (if configured) which is the best option.
			if err != nil {
				// Report
				promise.setState(Failed_Transport)

				if promise.rFunc != nil {
					go promise.rFunc(promise.msg, err)
				}

				ec <- BusError_IO
				close(ec)
				return
			}

			b.Reset()

			promise.setState(Sent)

			if promise.rFunc != nil {
				go promise.rFunc(promise.msg, nil)
			}
		}
	}(ctx, writer, wc, pwc, ec)

	return ec

}

func newReader(ctx *busContext) <-chan proto.Message {

	readChan := make(chan proto.Message)

	rs := make(chan struct{})
	ctx.rs = rs

	bufferedIO := bufio.NewReader(ctx.conn)

	// lets make a copy of prototype instance, just to be safe.
	protoMessage := proto.Clone(ctx.Endpoint().PrototypeInstance())

	go func(readChan chan<- proto.Message, rs <-chan struct{}, b *bufio.Reader) {

		defer close(readChan)
		defer log.Println("Reader exiting for context:", ctx)

		for {

			select {

			case <-rs:
				// Reader starts the shutdown process via closing its readChan.
				// Upon receiving closed on readChan, parent goroutine closes the writeChan which shuts down
				// the writer.
				return

			default:

				// Golang's socket read timeouts works iteratively, so every read call to connection has to be
				// individually configured.

				ctx.conn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
				err := read(b, protoMessage, readChan)

				// All types of transport errors are unrecoverable, leaving the stream in a inconsistent state.
				// One exception here is the timeout err which is being handled in read function and returned as nil.
				if err != nil {
					ctx.setState(NetworkClosed)
					return
				}
			}

		}
	}(readChan, rs, bufferedIO)

	return readChan

}

func read(b *bufio.Reader, m proto.Message, readChan chan<- proto.Message) error {

	messageLength, err := binary.ReadUvarint(b)

	if err != nil {
		return wrapReadErr(err)
	}

	messageBody := make([]byte, messageLength)

	var read int = 0

	for {
		read, err = b.Read(messageBody)

		if err != nil {
			return wrapReadErr(err)
		}

		if read == int(messageLength) {
			// we got all we need
			break
		}

	}

	err = proto.Unmarshal(messageBody, m)

	if err != nil {
		log.Println(err)
		return BusError_FailedUnmarshalling
	}

	readChan <- m

	return nil

}

func wrapReadErr(err error) error {

	if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
		return nil
	} else {
		return BusError_IO
	}

}
