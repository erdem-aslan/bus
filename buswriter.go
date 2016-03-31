package bus

import (
	"bufio"
	"github.com/golang/protobuf/proto"
	"time"
)

func startNewWriter(ctx *socketContext) (<-chan error, chan<- *busPromise, chan<- *busPromise) {

	bs := ctx.Endpoint().BufferSize

	if bs < 0 {
		bs = 0
	}

	wc := make(chan *busPromise, bs)
	pwc := make(chan *busPromise, bs)

	wec := make(chan error)

	writer := bufio.NewWriter(ctx.conn)

	ctx.ws = make(chan struct{})

	go func() {

		defer ctx.conn.Close()

		b := proto.NewBuffer(make([]byte, 0, 4096))

	infinite:
		for {

			select {
			case <-ctx.ws:

				switch ctx.State() {

				case UserClosed:

					drainWithError(pwc, wc)

				case UserClosing:

					for promise := range pwc {
						write(promise, b, writer)
					}

					for promise := range wc {
						write(promise, b, writer)
					}
				}

				break infinite

			default:
				select {

				case promise, ok := <-pwc:
					if ok {

						err := write(promise, b, writer)
						if err != nil {
							wec <- err
						}
					}
				case promise, ok := <-wc:
					if ok {
						err := write(promise, b, writer)

						if err != nil {
							wec <- err
						}

					}
				}
			}
		}
	}()

	return wec, wc, pwc

}

func write(promise *busPromise, b *proto.Buffer, writer *bufio.Writer) (err error) {
	if promise.State() == Cancelled {
		return nil
	}

	if !promise.validUntil.IsZero() && promise.validUntil.Before(time.Now()) {

		promise.setState(FailedTimeout, BusError_DeliveryTimeout)
		return nil
	}

	err = b.EncodeMessage(promise.msg)

	if err != nil {

		promise.setState(FailedSerialization, BusError_FailedMarshaling)

		b.Reset()

		return BusError_FailedMarshaling

	}

	_, writeErr := writer.Write(b.Bytes())

	if writeErr != nil {
		b.Reset()
		promise.setState(FailedTransport, BusError_IO)
		return BusError_IO
	}

	err = writer.Flush()

	// write error is fatal, remote endpoint may try to decode malformed bytes.
	if err != nil {
		b.Reset()
		promise.setState(FailedTransport, BusError_IO)
		return BusError_IO
	}

	b.Reset()

	promise.setState(Sent, nil)

	return nil

}

func drainWithError(pwc chan *busPromise, wc chan *busPromise) {

	for promise := range pwc {

		if promise.rFunc != nil {
			promise.rFunc(promise.msg, BusError_ContextClosed)
		}
	}

	for promise := range wc {

		if promise.rFunc != nil {
			promise.rFunc(promise.msg, BusError_ContextClosed)
		}
	}
}
