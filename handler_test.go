package gotftp

import (
	"bytes"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type rwcBuffer struct {
	*bytes.Buffer
}

func (r *rwcBuffer) Close() error {
	return nil
}

type handlerContext struct {
	snd chan interface{}
	rcv chan packet

	readFunc  func(peer net.Addr, filename string) (ReadCloser, error)
	writeFunc func(peer net.Addr, filename string) (WriteCloser, error)
}

func newHandlerContext() *handlerContext {
	h := &handlerContext{
		snd: make(chan interface{}, 1),
		rcv: make(chan packet, 1),
	}
	go serve(h, h, h)
	return h
}

// To implement packetReader
func (h *handlerContext) read(timeout time.Duration) (packet, error) {
	select {
	case e, ok := <-h.snd:
		if !ok {
			return nil, ErrTimeout
		}

		switch t := e.(type) {
		case packet:
			return t, nil
		case error:
			return nil, t
		default:
			panic("")
		}
	}
}

// Implement packetWriter
func (h *handlerContext) write(p packet) error {
	h.rcv <- p
	return nil
}

// To implement Handler
func (h *handlerContext) ReadFile(peer net.Addr, filename string) (ReadCloser, error) {
	if h.readFunc == nil {
		return &rwcBuffer{&bytes.Buffer{}}, nil
	}
	return h.readFunc(peer, filename)
}

// To implement Handler
func (h *handlerContext) WriteFile(peer net.Addr, filename string) (WriteCloser, error) {
	if h.writeFunc == nil {
		return &rwcBuffer{&bytes.Buffer{}}, nil
	}
	return h.writeFunc(peer, filename)
}

func TestMalformedFirstPacket(t *testing.T) {
	h := newHandlerContext()
	h.snd <- errOpcode

	px := <-h.rcv
	assert.IsType(t, &packetERROR{}, px)

	p := px.(*packetERROR)
	assert.Equal(t, p.errorCode, opcode(0))
	assert.Equal(t, p.errorMessage, "invalid opcode")
}

func TestUnexpectedFirstPacket(t *testing.T) {
	h := newHandlerContext()
	h.snd <- &packetACK{blockNr: uint16(1337)}

	px := <-h.rcv
	assert.IsType(t, &packetERROR{}, px)

	p := px.(*packetERROR)
	assert.Equal(t, p.errorCode, opcode(4))
}

func TestReadFileError(t *testing.T) {
	var tests = []struct {
		p            packet
		errorCode    uint16
		errorMessage string
	}{
		{
			&packetRRQ{packetXRQ{filename: "NotExists"}},
			1,
			os.ErrNotExist.Error(),
		},
		{
			&packetRRQ{packetXRQ{filename: "Permission"}},
			2,
			os.ErrPermission.Error(),
		},
		{
			&packetRRQ{packetXRQ{filename: "Default"}},
			0,
			"",
		},
	}

	for _, test := range tests {
		h := newHandlerContext()
		h.readFunc = func(_ net.Addr, filename string) (ReadCloser, error) {
			switch filename {
			case "NotExists":
				return nil, os.ErrNotExist
			case "Permission":
				return nil, os.ErrPermission
			default:
				return nil, errors.New("")
			}
		}

		h.snd <- test.p
		px := <-h.rcv
		assert.IsType(t, &packetERROR{}, px)

		p := px.(*packetERROR)
		assert.Equal(t, p.errorCode, test.errorCode)
		assert.Equal(t, p.errorMessage, test.errorMessage)
	}
}

func TestReadRequestNegotiation(t *testing.T) {
	var tests = []struct {
		opt      string
		proposed string
		returned string

		errorCode    uint16
		errorMessage string
	}{
		{
			opt:      "blksize",
			proposed: "", // Empty
			returned: "",

			errorCode:    8,
			errorMessage: "invalid syntax",
		},
		{
			opt:      "blksize",
			proposed: "xxx", // Not a number
			returned: "",

			errorCode:    8,
			errorMessage: "invalid syntax",
		},
		{
			opt:      "blksize",
			proposed: "7",
			returned: "8",
		},
		{
			opt:      "blksize",
			proposed: "65536",
			returned: "65464",
		},
		{
			opt:      "blksize",
			proposed: "12345",
			returned: "12345",
		},
		{
			opt:      "timeout",
			proposed: "", // Empty
			returned: "",

			errorCode:    8,
			errorMessage: "invalid syntax",
		},
		{
			opt:      "timeout",
			proposed: "xxx", // Not a number
			returned: "",

			errorCode:    8,
			errorMessage: "invalid syntax",
		},
		{
			opt:      "timeout",
			proposed: "0",
			returned: "1",
		},
		{
			opt:      "timeout",
			proposed: "256",
			returned: "255",
		},
		{
			opt:      "timeout",
			proposed: "32",
			returned: "32",
		},
	}

	for _, test := range tests {
		h := newHandlerContext()

		p := &packetRRQ{
			packetXRQ{
				options: map[string]string{
					test.opt: test.proposed,
				},
			},
		}

		h.snd <- p
		px := <-h.rcv

		switch p := px.(type) {
		case *packetERROR:
			assert.Equal(t, p.errorCode, test.errorCode)
			assert.Contains(t, p.errorMessage, test.errorMessage)
		case (*packetOACK):
			// Send ACK as response to OACK
			h.snd <- &packetACK{blockNr: 0}

			value, ok := p.options[test.opt]
			assert.True(t, ok)
			assert.Equal(t, value, test.returned)
		}
	}
}

func TestReadRequestFile(t *testing.T) {
	h := newHandlerContext()

	buf := []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa}

	// Return reader of buf as file to serve up.
	h.readFunc = func(_ net.Addr, _ string) (ReadCloser, error) {
		return &rwcBuffer{bytes.NewBuffer(buf)}, nil
	}

	p := &packetRRQ{
		packetXRQ{
			options: map[string]string{
				"blksize": "8",
			},
		},
	}

	h.snd <- p

	// Receive and validate OACK
	poack := <-h.rcv
	assert.IsType(t, &packetOACK{}, poack)
	oack := poack.(*packetOACK)
	blksize, ok := oack.options["blksize"]
	assert.True(t, ok)
	assert.Equal(t, blksize, "8")

	// Send ACK as response to OACK.
	h.snd <- &packetACK{blockNr: 0}

	// DATA packets we expect to receive.
	packets := []*packetDATA{
		&packetDATA{blockNr: 1, data: buf[:8]},
		&packetDATA{blockNr: 2, data: buf[8:]},
	}

	for _, expected := range packets {
		pdata := <-h.rcv
		assert.IsType(t, &packetDATA{}, pdata)

		actual := pdata.(*packetDATA)
		assert.Equal(t, expected, actual)
		h.snd <- &packetACK{blockNr: actual.blockNr}
	}
}
