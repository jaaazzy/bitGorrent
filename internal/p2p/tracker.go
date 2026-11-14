package p2p

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

func discover(ctx context.Context, t TorrentFile, id [20]byte, port uint16, downloaded, uploaded, left int64, event string) ([]Peer, time.Duration, error) {
	var peers []Peer
	interval := 15 * time.Second
	var last error
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sources := t.Trackers
	if len(sources) > 12 {
		sources = sources[:12]
	}
	add := func(ps []Peer) {
		for _, p := range ps {
			if p.Port != 0 && !p.IP.IsUnspecified() && !p.IP.IsMulticast() && !seen[p.String()] {
				seen[p.String()] = true
				peers = append(peers, p)
			}
		}
	}
	for _, tracker := range sources {
		wg.Add(1)
		go func(tracker string) {
			defer wg.Done()
			ps, d, e := announce(ctx, tracker, t.InfoHash, id, port, downloaded, uploaded, left, event)
			mu.Lock()
			defer mu.Unlock()
			if e != nil {
				last = e
			} else {
				add(ps)
				interval = d
			}
		}(tracker)
	}
	// Public torrents can fall back to DHT. Private torrents never contact DHT.
	if !t.Private {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ps, e := dhtPeers(ctx, t.InfoHash, port)
			mu.Lock()
			defer mu.Unlock()
			add(ps)
			if e != nil {
				last = e
			}
		}()
	}
	wg.Wait()
	if len(peers) == 0 && last == nil {
		last = errors.New("no peers available")
	}
	return peers, interval, last
}
func announce(ctx context.Context, address string, hash, id [20]byte, port uint16, downloaded, uploaded, left int64, event string) ([]Peer, time.Duration, error) {
	u, e := url.Parse(address)
	if e != nil {
		return nil, 0, e
	}
	if u.Scheme == "udp" {
		return announceUDP(ctx, u, hash, id, port, downloaded, uploaded, left, event)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, 0, errors.New("unsupported tracker scheme")
	}
	q := u.Query()
	q.Set("info_hash", string(hash[:]))
	q.Set("peer_id", string(id[:]))
	q.Set("port", strconv.Itoa(int(port)))
	q.Set("uploaded", strconv.FormatInt(uploaded, 10))
	q.Set("downloaded", strconv.FormatInt(downloaded, 10))
	q.Set("left", strconv.FormatInt(left, 10))
	q.Set("compact", "1")
	q.Set("numwant", "40")
	if event != "" {
		q.Set("event", event)
	}
	u.RawQuery = q.Encode()
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return nil, 0, e
	}
	resp, e := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if e != nil {
		return nil, 0, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, 0, fmt.Errorf("tracker returned HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return nil, 0, e
	}
	v, e := decode(b)
	if e != nil {
		return nil, 0, e
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, 0, errors.New("invalid tracker response")
	}
	if fail := str(m["failure reason"]); fail != "" {
		return nil, 0, errors.New(fail)
	}
	var peers []Peer
	if compact, ok := m["peers"].(string); ok {
		peers, e = Unmarshal([]byte(compact))
	} else if list, ok := m["peers"].([]any); ok {
		for _, v := range list {
			p, ok := v.(map[string]any)
			if ok {
				ip := net.ParseIP(str(p["ip"]))
				port := integer(p["port"])
				if ip != nil && port > 0 && port <= 65535 {
					peers = append(peers, Peer{ip, uint16(port)})
				}
			}
		}
	}
	return peers, time.Duration(integer(m["interval"])) * time.Second, e
}
func udpExchange(ctx context.Context, conn net.Conn, request []byte, action uint32) ([]byte, error) {
	var transaction [4]byte
	if _, e := rand.Read(transaction[:]); e != nil {
		return nil, e
	}
	copy(request[12:16], transaction[:])
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if _, e := conn.Write(request); e != nil {
		return nil, e
	}
	b := make([]byte, 65536)
	n, e := conn.Read(b)
	if e != nil {
		return nil, e
	}
	b = b[:n]
	if n < 8 || !bytes.Equal(b[4:8], transaction[:]) {
		return nil, errors.New("invalid UDP tracker transaction")
	}
	if readUint(b[:4]) == 3 {
		return nil, errors.New(string(b[8:]))
	}
	if readUint(b[:4]) != action {
		return nil, errors.New("invalid tracker action")
	}
	return b, nil
}
func announceUDP(ctx context.Context, u *url.URL, hash, id [20]byte, port uint16, downloaded, uploaded, left int64, event string) ([]Peer, time.Duration, error) {
	conn, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "udp", u.Host)
	if e != nil {
		return nil, 0, e
	}
	defer conn.Close()
	req := make([]byte, 16)
	binary.BigEndian.PutUint64(req[:8], 0x41727101980)
	b, e := udpExchange(ctx, conn, req, 0)
	if e != nil {
		return nil, 0, e
	}
	if len(b) < 16 {
		return nil, 0, errors.New("short tracker connect response")
	}
	req = make([]byte, 98)
	copy(req[:8], b[8:16])
	binary.BigEndian.PutUint32(req[8:12], 1)
	copy(req[16:36], hash[:])
	copy(req[36:56], id[:])
	binary.BigEndian.PutUint64(req[56:64], uint64(downloaded))
	binary.BigEndian.PutUint64(req[64:72], uint64(left))
	binary.BigEndian.PutUint64(req[72:80], uint64(uploaded))
	events := map[string]uint32{"completed": 1, "started": 2, "stopped": 3}
	binary.BigEndian.PutUint32(req[80:84], events[event])
	rand.Read(req[88:92])
	binary.BigEndian.PutUint32(req[92:96], 40)
	binary.BigEndian.PutUint16(req[96:98], port)
	b, e = udpExchange(ctx, conn, req, 1)
	if e != nil {
		return nil, 0, e
	}
	if len(b) < 20 {
		return nil, 0, errors.New("short tracker announce response")
	}
	peers, e := Unmarshal(b[20:])
	return peers, time.Duration(readUint(b[8:12])) * time.Second, e
}
