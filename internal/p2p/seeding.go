package p2p

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

func readUint(b []byte) uint32 { return binary.BigEndian.Uint32(b) }
func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func waitContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type limiter struct {
	mu   sync.Mutex
	rate int64
	next time.Time
}

func newLimiter(rate int64) *limiter { return &limiter{rate: rate} }
func (l *limiter) wait(ctx context.Context, n int) error {
	if l.rate <= 0 {
		return ctx.Err()
	}
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	end := l.next.Add(time.Duration(float64(n) / float64(l.rate) * float64(time.Second)))
	l.next = end
	l.mu.Unlock()
	return waitContext(ctx, time.Until(end))
}

func serveSeeds(ctx context.Context, listener net.Listener, t TorrentFile, id [20]byte, s *storage, limit *limiter, uploaded func(int)) {
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	slots := make(chan struct{}, 16)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			defer conn.Close()
			stop := context.AfterFunc(ctx, func() { conn.Close() })
			defer stop()
			conn.SetDeadline(time.Now().Add(10 * time.Second))
			h, e := ReadHandshake(conn)
			if e != nil || h.InfoHash != t.InfoHash {
				return
			}
			if writeFull(conn, NewHandshake(t.InfoHash, id).Serialize()) != nil {
				return
			}
			if writeFull(conn, (&Message{ID: MsgBitfield, Payload: s.bitfield()}).Serialize()) != nil {
				return
			}
			for {
				conn.SetDeadline(time.Now().Add(60 * time.Second))
				m, e := ReadMessage(conn)
				if e != nil {
					return
				}
				if m == nil {
					continue
				}
				switch m.ID {
				case MsgInterested:
					if writeFull(conn, (&Message{ID: MsgUnchoke}).Serialize()) != nil {
						return
					}
				case MsgRequest:
					if len(m.Payload) != 12 {
						return
					}
					index, begin, n := int(readUint(m.Payload[:4])), int(readUint(m.Payload[4:8])), int(readUint(m.Payload[8:]))
					b, e := s.block(index, begin, n)
					if e != nil {
						return
					}
					if limit.wait(ctx, len(b)) != nil {
						return
					}
					payload := make([]byte, 8+len(b))
					copy(payload, m.Payload[:8])
					copy(payload[8:], b)
					conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if writeFull(conn, (&Message{ID: MsgPiece, Payload: payload}).Serialize()) != nil {
						return
					}
					uploaded(len(b))
				}
			}
		}()
	}
}
