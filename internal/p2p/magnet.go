package p2p

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jackpal/bencode-go"
)

type Magnet struct {
	Hash     [20]byte
	Name     string
	Trackers []string
}

func ParseMagnet(source string) (Magnet, error) {
	var m Magnet
	u, e := url.Parse(source)
	if e != nil || u.Scheme != "magnet" {
		return m, errors.New("enter a valid magnet link")
	}
	q := u.Query()
	var encoded string
	for _, xt := range q["xt"] {
		if strings.HasPrefix(xt, "urn:btih:") {
			encoded = strings.TrimPrefix(xt, "urn:btih:")
			break
		}
	}
	var raw []byte
	switch len(encoded) {
	case 40:
		raw, e = hex.DecodeString(encoded)
	case 32:
		raw, e = base32.StdEncoding.DecodeString(strings.ToUpper(encoded))
	default:
		return m, errors.New("magnet must contain a v1 btih info hash")
	}
	if e != nil || len(raw) != 20 {
		return m, errors.New("invalid magnet hash")
	}
	copy(m.Hash[:], raw)
	m.Name = q.Get("dn")
	m.Trackers = q["tr"]
	return m, nil
}
func ResolveMagnet(ctx context.Context, source string, report func(string)) (TorrentFile, error) {
	m, e := ParseMagnet(source)
	if e != nil {
		return TorrentFile{}, e
	}
	var id [20]byte
	rand.Read(id[:])
	placeholder := TorrentFile{InfoHash: m.Hash, Trackers: m.Trackers}
	for {
		if e = ctx.Err(); e != nil {
			return TorrentFile{}, e
		}
		report("Finding metadata peers")
		peers, _, _ := discover(ctx, placeholder, id, 0, 0, 0, 1, "")
		if len(peers) > 12 {
			peers = peers[:12]
		}
		for _, peer := range peers {
			raw, e := metadataFromPeer(ctx, peer, m.Hash, id)
			if e != nil {
				continue
			}
			if sha1.Sum(raw) != m.Hash {
				continue
			}
			t, e := parseInfo(raw)
			if e != nil {
				return t, e
			}
			t.Trackers = m.Trackers
			if len(m.Trackers) > 0 {
				t.Announce = m.Trackers[0]
			}
			return t, nil
		}
		report("No metadata peers available; retrying")
		if e = waitContext(ctx, 5*time.Second); e != nil {
			return TorrentFile{}, e
		}
	}
}
func metadataFromPeer(ctx context.Context, peer Peer, hash, id [20]byte) ([]byte, error) {
	conn, e := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", peer.String())
	if e != nil {
		return nil, e
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	handshake := NewHandshake(hash, id).Serialize()
	handshake[25] |= 0x10
	if e = writeFull(conn, handshake); e != nil {
		return nil, e
	}
	h, e := ReadHandshake(conn)
	if e != nil || h.InfoHash != hash {
		return nil, errors.New("invalid metadata handshake")
	}
	var b bytes.Buffer
	bencode.Marshal(&b, map[string]any{"m": map[string]any{"ut_metadata": 1}})
	if e = writeFull(conn, (&Message{ID: 20, Payload: append([]byte{0}, b.Bytes()...)}).Serialize()); e != nil {
		return nil, e
	}
	extension, size := 0, 0
	for i := 0; i < 32; i++ {
		msg, e := ReadMessage(conn)
		if e != nil {
			return nil, e
		}
		if msg == nil || msg.ID != 20 || len(msg.Payload) < 2 || msg.Payload[0] != 0 {
			continue
		}
		v, e := decode(msg.Payload[1:])
		if e != nil {
			return nil, e
		}
		dict, ok := v.(map[string]any)
		if !ok {
			continue
		}
		ext, _ := dict["m"].(map[string]any)
		extension = int(integer(ext["ut_metadata"]))
		size = int(integer(dict["metadata_size"]))
		break
	}
	if extension <= 0 || extension > 255 || size <= 0 || size > maxMetadata {
		return nil, errors.New("peer does not support bounded metadata exchange")
	}
	raw := make([]byte, 0, size)
	for piece := 0; len(raw) < size; piece++ {
		b.Reset()
		bencode.Marshal(&b, map[string]any{"msg_type": 0, "piece": piece})
		if e = writeFull(conn, (&Message{ID: 20, Payload: append([]byte{byte(extension)}, b.Bytes()...)}).Serialize()); e != nil {
			return nil, e
		}
		found := false
		for i := 0; i < 64; i++ {
			msg, e := ReadMessage(conn)
			if e != nil {
				return nil, e
			}
			if msg == nil || msg.ID != 20 || len(msg.Payload) < 2 || msg.Payload[0] != 1 {
				continue
			}
			d := decoder{data: msg.Payload[1:]}
			v, e := d.value(0)
			if e != nil {
				return nil, e
			}
			header, ok := v.(map[string]any)
			if !ok || integer(header["msg_type"]) != 1 || integer(header["piece"]) != int64(piece) {
				return nil, errors.New("metadata request rejected")
			}
			chunk := msg.Payload[1+d.pos:]
			expected := min(MaxBlockSize, size-len(raw))
			if len(chunk) != expected {
				return nil, errors.New("invalid metadata block size")
			}
			raw = append(raw, chunk...)
			found = true
			break
		}
		if !found {
			return nil, errors.New("missing metadata block")
		}
	}
	return raw, nil
}
