package p2p

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const MaxBlockSize = 16384
const MaxBacklog = 5

type Progress struct {
	State         string  `json:"state"`
	Completed     int64   `json:"completed"`
	Total         int64   `json:"total"`
	Downloaded    int64   `json:"downloaded"`
	Uploaded      int64   `json:"uploaded"`
	DownloadSpeed int64   `json:"downloadSpeed"`
	UploadSpeed   int64   `json:"uploadSpeed"`
	Peers         int     `json:"peers"`
	Message       string  `json:"message"`
	FileCompleted []int64 `json:"fileCompleted"`
}
type RunOptions struct {
	Metadata        TorrentFile
	Destination     string
	Selected        []int
	SeedRatio       float64
	DownloadLimit   int64
	UploadLimit     int64
	CheckOnly       bool
	InitialUploaded int64
}
type Engine struct{}

func (Engine) Run(ctx context.Context, o RunOptions, report func(Progress)) error {
	s, err := openStorage(o.Metadata, o.Destination, o.Selected)
	if err != nil {
		return err
	}
	defer s.close()
	t := o.Metadata
	p := Progress{State: "checking", Total: s.total(), Uploaded: o.InitialUploaded}
	report(p)
	if err = s.recheck(ctx, func(n int64) { p.Completed = n; p.FileCompleted = s.fileProgress(); report(p) }); err != nil {
		return err
	}
	if o.CheckOnly {
		return nil
	}
	defer s.checkpoint()
	var peerID [20]byte
	if _, err = rand.Read(peerID[:]); err != nil {
		return err
	}
	copy(peerID[:8], "-BG0100-")
	dl, ul := newLimiter(o.DownloadLimit), newLimiter(o.UploadLimit)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var uploadMu sync.Mutex
	uploaded := o.InitialUploaded
	listener, listenErr := net.Listen("tcp", ":6881")
	if listenErr != nil {
		listener, listenErr = net.Listen("tcp", ":0")
	}
	port := uint16(0)
	var seedWG sync.WaitGroup
	if listenErr == nil {
		port = uint16(listener.Addr().(*net.TCPAddr).Port)
		seedWG.Add(1)
		go func() {
			defer seedWG.Done()
			serveSeeds(runCtx, listener, t, peerID, s, ul, func(n int) { uploadMu.Lock(); uploaded += int64(n); uploadMu.Unlock() })
		}()
	}
	defer func() {
		cancel()
		if listener != nil {
			listener.Close()
		}
		seedWG.Wait()
	}()
	last := time.Now()
	var lastDown int64
	lastUp := uploaded
	emit := func(state, message string, peers int) {
		now := time.Now()
		uploadMu.Lock()
		p.Uploaded = uploaded
		uploadMu.Unlock()
		p.State = state
		p.Message = message
		p.Peers = peers
		p.Completed = s.completed()
		p.Downloaded = s.received()
		p.FileCompleted = s.fileProgress()
		if seconds := now.Sub(last).Seconds(); seconds >= 0.25 {
			p.DownloadSpeed = int64(float64(p.Downloaded-lastDown) / seconds)
			p.UploadSpeed = int64(float64(p.Uploaded-lastUp) / seconds)
			last = now
			lastDown = p.Downloaded
			lastUp = p.Uploaded
		}
		report(p)
	}
	trackerEvent := "started"
	var peers []Peer
	var nextDiscovery time.Time
	pieceSlots := make(chan struct{}, max(1, min(8, int((32<<20)/t.PieceLength))))
	for !s.complete() {
		if err = ctx.Err(); err != nil {
			return err
		}
		emit("downloading", "Finding peers", 0)
		if time.Now().After(nextDiscovery) {
			found, interval, e := discover(ctx, t, peerID, port, s.received(), p.Uploaded, t.Length-s.allCompleted(), trackerEvent)
			trackerEvent = ""
			if len(found) > 0 {
				peers = found
			}
			if interval < 5*time.Second {
				interval = 5 * time.Second
			}
			nextDiscovery = time.Now().Add(interval)
			if e != nil && len(peers) == 0 {
				emit("waiting", e.Error(), 0)
			}
		}
		if len(peers) > 40 {
			peers = peers[:40]
		}
		var wg sync.WaitGroup
		var activeMu sync.Mutex
		active := 0
		diskErrors := make(chan error, 1)
		for _, peer := range peers {
			wg.Add(1)
			go func(peer Peer) {
				defer wg.Done()
				c, e := connectContext(runCtx, peer, peerID, t.InfoHash)
				if e != nil {
					return
				}
				defer c.Conn.Close()
				stop := context.AfterFunc(runCtx, func() { c.Conn.Close() })
				defer stop()
				activeMu.Lock()
				active++
				activeMu.Unlock()
				defer func() { activeMu.Lock(); active--; activeMu.Unlock() }()
				if c.SendInterested() != nil {
					return
				}
				for {
					select {
					case pieceSlots <- struct{}{}:
					case <-runCtx.Done():
						return
					}
					index := s.claim(c.Bitfield)
					if index < 0 {
						<-pieceSlots
						return
					}
					buf, e := fetchPiece(runCtx, c, index, s.pieceSize(index), dl)
					if e == nil && sha1.Sum(buf) != t.PieceHashes[index] {
						e = errors.New("corrupt piece")
					}
					if e == nil {
						if e = s.commit(index, buf); e != nil {
							select {
							case diskErrors <- e:
							default:
							}
							cancel()
						}
					}
					s.release(index)
					<-pieceSlots
					if e != nil {
						return
					}
					if c.SendHave(index) != nil {
						return
					}
				}
			}(peer)
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		ticker := time.NewTicker(500 * time.Millisecond)
		for running := true; running; {
			select {
			case <-done:
				running = false
			case <-ticker.C:
				activeMu.Lock()
				n := active
				activeMu.Unlock()
				emit("downloading", "", n)
			case <-runCtx.Done():
				cancel()
				<-done
				ticker.Stop()
				select {
				case e := <-diskErrors:
					return e
				default:
					return ctx.Err()
				}
			}
		}
		ticker.Stop()
		select {
		case e := <-diskErrors:
			return e
		default:
		}
		if s.complete() {
			break
		}
		emit("waiting", "Waiting for available pieces; retrying discovery", 0)
		if err = waitContext(ctx, 5*time.Second); err != nil {
			return err
		}
	}
	if err = s.checkpoint(); err != nil {
		return err
	}
	if err = s.finalize(); err != nil {
		return err
	}
	emit("completed", "Download verified", 0)
	if o.SeedRatio <= 0 {
		return nil
	}
	if listenErr != nil {
		return fmt.Errorf("download complete; cannot seed: %w", listenErr)
	}
	event := ""
	if s.allCompleted() == t.Length {
		event = "completed"
	}
	_, _, _ = discover(ctx, t, peerID, port, s.received(), p.Uploaded, t.Length-s.allCompleted(), event)
	announce := time.NewTicker(60 * time.Second)
	defer announce.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		emit("seeding", "", 0)
		if p.Total == 0 || float64(p.Uploaded) >= float64(p.Total)*o.SeedRatio {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		case <-announce.C:
			_, _, _ = discover(ctx, t, peerID, port, s.received(), p.Uploaded, t.Length-s.allCompleted(), "")
		}
	}
}
func connectContext(ctx context.Context, peer Peer, id, hash [20]byte) (*Client, error) {
	conn, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", peer.String())
	if e != nil {
		return nil, e
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if _, e = completeHandshake(conn, hash, id); e != nil {
		conn.Close()
		return nil, e
	}
	c := &Client{Conn: conn, Choked: true, peer: peer, infoHash: hash, peerID: id}
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	for i := 0; i < 32; i++ {
		m, e := ReadMessage(conn)
		if e != nil {
			conn.Close()
			return nil, e
		}
		if m == nil {
			continue
		}
		switch m.ID {
		case MsgUnchoke:
			c.Choked = false
		case MsgBitfield:
			c.Bitfield = Bitfield(m.Payload)
			conn.SetDeadline(time.Time{})
			return c, nil
		case MsgHave:
			c.Bitfield = make(Bitfield, 1<<16)
			n, e := ParseHave(m)
			if e == nil {
				c.Bitfield.SetPiece(n)
			}
			conn.SetDeadline(time.Time{})
			return c, nil
		}
	}
	conn.Close()
	return nil, errors.New("peer did not advertise pieces")
}
func fetchPiece(ctx context.Context, c *Client, index, size int, l *limiter) ([]byte, error) {
	buf := make([]byte, size)
	requested := make(map[int]int)
	next, done := 0, 0
	c.Conn.SetDeadline(time.Now().Add(30 * time.Second))
	for done < size {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		for !c.Choked && len(requested) < MaxBacklog && next < size {
			n := min(MaxBlockSize, size-next)
			if e := l.wait(ctx, n); e != nil {
				return nil, e
			}
			c.Conn.SetDeadline(time.Now().Add(30 * time.Second))
			if e := c.SendRequest(index, next, n); e != nil {
				return nil, e
			}
			requested[next] = n
			next += n
		}
		c.Conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		m, e := ReadMessage(c.Conn)
		if e != nil {
			return nil, e
		}
		if m == nil {
			continue
		}
		switch m.ID {
		case MsgChoke:
			c.Choked = true
		case MsgUnchoke:
			c.Choked = false
		case MsgHave:
			n, e := ParseHave(m)
			if e != nil {
				return nil, e
			}
			c.Bitfield.SetPiece(n)
		case MsgPiece:
			if len(m.Payload) < 8 {
				return nil, errors.New("short piece message")
			}
			begin := int(readUint(m.Payload[4:8]))
			n, ok := requested[begin]
			if !ok || len(m.Payload)-8 != n {
				return nil, errors.New("unsolicited or duplicate block")
			}
			if _, e = ParsePiece(index, buf, m); e != nil {
				return nil, e
			}
			delete(requested, begin)
			done += n
		}
	}
	return buf, nil
}
