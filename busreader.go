package bus

import (
	"bufio"
	"encoding/binary"
	"github.com/golang/protobuf/proto"
)

func startNewReader(ctx *socketContext) <-chan proto.Message {

	rc := make(chan proto.Message)
	ctx.rs = make(chan struct{})

	go func(ctx *socketContext, rc chan proto.Message) {

		reader := bufio.NewReader(ctx.conn)
		protoMessage := proto.Clone(ctx.Endpoint().PrototypeInstance())

		for {
			select {

			case <-ctx.rs:
				return

			default:

				length, frErr := readFrame(ctx)

				if frErr != nil {
					ctx.setState(NetworkClosed)
					close(rc)
					return
				}

				err := readBody(length, reader, protoMessage, rc)

				if err != nil {
					ctx.setState(NetworkClosed)
					close(rc)
					return
				}
			}
		}
	}(ctx, rc)

	return rc

}

func readFrame(ctx *socketContext) (uint64, error) {

	singleByte := make([]byte, 1)
	maxBuffer := make([]byte, 0, binary.MaxVarintLen64)

	for {
		read, err := ctx.conn.Read(singleByte)


		if err != nil {
			return 0, err
		}

		if read == 0 {
			return 0, BusError_IO
		}

		maxBuffer = append(maxBuffer, singleByte...)

		hasMoreBytes := singleByte[0]&0x80 != 0

		if !hasMoreBytes {
			length, _ := proto.DecodeVarint(maxBuffer)

			if length == 0 {
				return 0, BusError_IO
			}
			return length, nil
		}
	}
}

func readBody(length uint64, b *bufio.Reader, m proto.Message, readChan chan<- proto.Message) error {

	messageBody := make([]byte, length)

	var count int

	for {
		r, err := b.Read(messageBody)

		if err != nil {
			return BusError_IO
		}

		count += r

		if count == int(length) {
			break
		}

	}

	err := proto.Unmarshal(messageBody, m)

	if err != nil {
		return BusError_FailedUnmarshalling
	}

	readChan <- m

	return nil

}
