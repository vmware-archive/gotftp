package gotftp

import (
	"bytes"
	"net"
	"sync"
	"time"
)

type packetReaderImpl struct {
	ch <-chan []byte
}

func (p *packetReaderImpl) read(timeout time.Duration) (packet, error) {
	select {
	case buf := <-p.ch:
		return packetFromWire(bytes.NewBuffer(buf))
	case <-time.After(timeout):
		return nil, ErrTimeout
	}
}

type packetWriterImpl struct {
	net.PacketConn

	addr net.Addr
	b    bytes.Buffer
}

func (p *packetWriterImpl) write(x packet) error {
	p.b.Reset()

	err := packetToWire(x, &p.b)
	if err != nil {
		return err
	}

	_, err = p.PacketConn.WriteTo(p.b.Bytes(), p.addr)
	return err
}

type syncPacketConn struct {
	net.PacketConn
	sync.Mutex
}

func (s *syncPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	s.Lock()
	n, err := s.PacketConn.WriteTo(b, addr)
	s.Unlock()
	return n, err
}

func Serve(l net.PacketConn, h Handler) error {
	lock := sync.Mutex{}
	table := make(map[string]chan []byte)
	buf := make([]byte, 65536)

	for {
		n, addr, err := l.ReadFrom(buf)
		if err != nil {
			return err
		}

		// Ownership of this buffer is transferred to the goroutine for the peer
		// address, so we need to make a copy before handing it off.
		b := make([]byte, n)
		copy(b, buf[:n])

		lock.Lock()

		ch, ok := table[addr.String()]
		if !ok {
			ch = make(chan []byte, 10)
			table[addr.String()] = ch

			// Packet reader for client
			r := &packetReaderImpl{
				ch: ch,
			}

			// Packet writer for client
			w := &packetWriterImpl{
				PacketConn: &syncPacketConn{
					PacketConn: l,
				},
				addr: addr,
			}

			// Kick off a serve loop for this peer address.
			go func() {
				// A client MAY reuse its socket for more than one request.
				// Therefore, continue running the serve loop until there are no more
				// inbound packets on the channel for this peer address.
				for stop := false; !stop; {
					serve(addr, r, w, h)

					lock.Lock()
					if len(ch) == 0 {
						delete(table, addr.String())
						stop = true
					}
					lock.Unlock()
				}
			}()
		}

		select {
		case ch <- b:
		default:
			// Drop packet on the floor if we can't handle it
		}

		// Unlock after sending buffer so that other routines can reliably check
		// and use the length of a channel while holding the lock.
		lock.Unlock()
	}
}

func ListenAndServe(h Handler) error {
	l, err := net.ListenPacket("udp4", ":69")
	if err != nil {
		return err
	}

	return Serve(l, h)
}
