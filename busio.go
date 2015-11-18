package bus

import (
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	"net"
	"time"
)

func dial(transport string, addr string, ctx *socketContext) error {

	conn, err := net.Dial(transport, addr)

	if err != nil {
		return err
	}

	ctx.conn = conn
	ctx.rcCount = 0

	startMainLoop(ctx)

	return nil

}

func redial(ctx *socketContext) {

	if ctx.State() != Reopening {
		return
	}

	ctx.rcCount++

	err := dial(ctx.e.Transport(), ctx.resolvedDest, ctx)

	if err != nil {

		_, c, d := ctx.Endpoint().ShouldReconnect()

		if c == 0 || ctx.rcCount < c {

			ctx.setState(Reopening)

			time.AfterFunc(d, func() {
				redial(ctx)
			})
		}
	}
}

func serve(e Endpoint) error {

	var err error

	switch e.Transport() {
	case "tcp":
		err = serveTcp(e)
	case "udp":
		err = serveUdp(e)
	case "ws":
		err = serveWs(e)
	default:
		err = errors.New(fmt.Sprintf("Unhandled transport type for serving: %s", e.Transport()))
	}

	return err
}

func serveTcp(e Endpoint) error {

	address, err := resolveAddress(e)

	if err != nil {
		return err
	}

	l, lErr := net.Listen("tcp", address)

	if lErr != nil {
		return lErr
	}

	quit := make(chan struct{}, 1)

	elLock.Lock()

	endpointListeners[e.Id()] = &listenerShutdown{
		l: l,
		q: quit,
	}

	elLock.Unlock()

	go accept(l, e, quit)

	return nil

}

func serveUdp(e Endpoint) error {
	address, err := resolveAddress(e)

	if err != nil {
		return err
	}

	l, lErr := net.Listen("udp", address)

	if lErr != nil {
		return lErr
	}

	quit := make(chan struct{}, 1)

	elLock.Lock()

	endpointListeners[e.Id()] = &listenerShutdown{
		l: l,
		q: quit,
	}

	elLock.Unlock()

	go accept(l, e, quit)

	return nil

}

func serveWs(e Endpoint) error {
	//@todo
	return nil
}

func accept(l net.Listener, e Endpoint, quit <-chan struct{}) {

infinite:
	for {

		conn, err := l.Accept()

		if err != nil {
			// we can't be sure if listener is closed and error is errClosing
			// we'll be using a chan for a listener close pre check

			select {
			case <-quit:
				// Listener is closed, all we need to do is stop.
				break infinite

			default:
				// default block for not blocking quit chan case.
			}

			continue
		}

		ctx := &socketContext{
			conn:     conn,
			ctxId:    e.Id() + "-" + conn.RemoteAddr().String() + "-" + e.Transport(),
			e:        e,
			ctxQueue: make(chan *busPromise, e.BufferSize()),
			served:   true,
			ctxQuit:  make(chan struct{}),
			netQuit:  make(chan struct{}),
		}

		scLock.Lock()
		defer scLock.Unlock()
		servedContexts[ctx.ctxId] = ctx

		ctx.setState(Opening)

		go startMainLoop(ctx)

	}
}

func startMainLoop(ctx *socketContext) {

	wec, wc, pwc := startNewWriter(ctx)
	rc := startNewReader(ctx)

	go func(ctx *socketContext) {

		var readOpen bool
		var message proto.Message

	infinite:
		for {
			select {
			case message, readOpen = <-rc:

				if !readOpen {

					if message != nil {
						go ctx.e.MessageHandler().HandleMessage(ctx, message)
					}

					ctx.netQuit <- struct{}{}

					if !ctx.served {

						r, c, d := ctx.Endpoint().ShouldReconnect()

						if r && (c == 0 || ctx.rcCount < c) {

							ctx.setState(Reopening)

							time.AfterFunc(d, func() {
								redial(ctx)
							})
						}

					}

					break infinite
				}

				go ctx.e.MessageHandler().HandleMessage(ctx, message)
			}
		}

	}(ctx)

	go func(ctx *socketContext) {

	infinite:
		for {
			select {

			case promise, open := <-ctx.ctxQueue:

				if open {
					if promise.urgent {
						pwc <- promise
					} else {
						wc <- promise
					}
				}

			case <-ctx.netQuit:

				quitWriter(ctx, pwc, wc)

				break infinite

			case <-ctx.ctxQuit:

				quitReader(ctx)
				quitWriter(ctx, pwc, wc)

				break infinite

			case <-wec:

				quitReader(ctx)
				quitWriter(ctx, pwc, wc)

				break infinite
			}
		}

	}(ctx)

	ctx.setState(Open)

}

func quitWriter(ctx *socketContext, pwc chan<- *busPromise, wc chan<- *busPromise) {

	close(pwc)
	close(wc)
	ctx.ws <- struct{}{}

	scLock.Lock()
	delete(servedContexts, ctx.ctxId)
	scLock.Unlock()

}

func quitReader(ctx *socketContext) {
	ctx.rs <- struct{}{}
}
