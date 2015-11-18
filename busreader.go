package bus

import (
	"bufio"
	"encoding/binary"
	"github.com/golang/protobuf/proto"
	"net"
)

func startNewReader(ctx *socketContext) <-chan proto.Message {

	rc := make(chan proto.Message)
	ctx.rs = make(chan struct{})

	bufferedIO := bufio.NewReader(ctx.conn)

	protoMessage := proto.Clone(ctx.Endpoint().PrototypeInstance())

	go func() {

		for {

			select {

			case <-ctx.rs:
				return

			default:

				err := read(bufferedIO, protoMessage, rc)

				if err != nil {
					ctx.setState(NetworkClosed)
					close(rc)
					return
				}
			}
		}
	}()

	return rc

}

func read(b *bufio.Reader, m proto.Message, readChan chan<- proto.Message) error {

	messageLength, err := binary.ReadUvarint(b)

	if err != nil {
		return wrapReadErr(err)
	}

	messageBody := make([]byte, messageLength)

	var count uint64

	// There are multiple ways for draining network buffer
	// and this is one of them (very opinionated one).
	for {
		r, err := b.Read(messageBody)

		if err != nil {
			return BusError_IO
		}

		count += uint64(r)

		if count == messageLength {
			break
		}

	}

	err = proto.Unmarshal(messageBody, m)

	if err != nil {
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
