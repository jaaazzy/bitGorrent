package p2p

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"sort"
	"time"

	"github.com/jackpal/bencode-go"
)

// Bounded outbound DHT lookup. It does not act as a public routing node.
func dhtPeers(ctx context.Context, hash [20]byte, port uint16) ([]Peer, error) {
	return lookupDHT(ctx, hash, port, []string{"router.bittorrent.com:6881", "router.utorrent.com:6881", "dht.transmissionbt.com:6881"})
}
func lookupDHT(ctx context.Context, hash [20]byte, port uint16, nodes []string) ([]Peer, error) {
	lookup, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var id [20]byte
	if _, e := rand.Read(id[:]); e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	var peers []Peer
	type candidate struct {
		address  string
		distance [20]byte
	}
	var candidates []candidate
	for attempts := 0; len(nodes) > 0 && attempts < 20; attempts++ {
		if lookup.Err() != nil {
			break
		}
		address := nodes[0]
		nodes = nodes[1:]
		if seen[address] {
			continue
		}
		seen[address] = true
		conn, e := (&net.Dialer{Timeout: time.Second}).DialContext(lookup, "udp4", address)
		if e != nil {
			continue
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		stop := context.AfterFunc(lookup, func() { conn.Close() })
		var tx [2]byte
		rand.Read(tx[:])
		var request bytes.Buffer
		e = bencode.Marshal(&request, map[string]any{"t": string(tx[:]), "y": "q", "q": "get_peers", "a": map[string]any{"id": string(id[:]), "info_hash": string(hash[:])}})
		if e == nil {
			_, e = conn.Write(request.Bytes())
		}
		buf := make([]byte, 4096)
		var n int
		if e == nil {
			n, e = conn.Read(buf)
		}
		stop()
		conn.Close()
		if e != nil {
			continue
		}
		v, e := decode(buf[:n])
		if e != nil {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok || str(m["t"]) != string(tx[:]) || str(m["y"]) != "r" {
			continue
		}
		r, ok := m["r"].(map[string]any)
		if !ok {
			continue
		}
		if port > 0 && str(r["token"]) != "" {
			dhtAnnounce(lookup, address, id, hash, str(r["token"]), port)
		}
		if values, ok := r["values"].([]any); ok {
			for _, v := range values {
				ps, e := Unmarshal([]byte(str(v)))
				if e == nil {
					peers = append(peers, ps...)
				}
			}
		}
		if len(peers) >= 40 {
			return peers, nil
		}
		compact := []byte(str(r["nodes"]))
		for len(compact) >= 26 {
			var distance [20]byte
			for i := range distance {
				distance[i] = compact[i] ^ hash[i]
			}
			p := Peer{net.IP(append([]byte(nil), compact[20:24]...)), binary.BigEndian.Uint16(compact[24:26])}
			if p.Port != 0 && !seen[p.String()] {
				candidates = append(candidates, candidate{p.String(), distance})
			}
			compact = compact[26:]
		}
		sort.Slice(candidates, func(i, j int) bool { return bytes.Compare(candidates[i].distance[:], candidates[j].distance[:]) < 0 })
		for _, c := range candidates {
			if len(nodes) < 40 && !seen[c.address] {
				nodes = append(nodes, c.address)
			}
		}
		candidates = nil
	}
	if len(peers) > 0 {
		return peers, nil
	}
	return nil, errors.New("DHT found no peers; retrying")
}

func dhtAnnounce(ctx context.Context, address string, id, hash [20]byte, token string, port uint16) {
	conn, e := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "udp4", address)
	if e != nil {
		return
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(time.Second))
	var request bytes.Buffer
	if bencode.Marshal(&request, map[string]any{"t": "bg", "y": "q", "q": "announce_peer", "a": map[string]any{"id": string(id[:]), "info_hash": string(hash[:]), "port": int(port), "token": token}}) == nil {
		conn.Write(request.Bytes())
	}
}
