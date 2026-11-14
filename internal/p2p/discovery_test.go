package p2p

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/jackpal/bencode-go"
)

func TestUDPTracker(t *testing.T) {
	conn, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	done := make(chan struct{})
	var hash, id [20]byte
	hash[0] = 7
	id[0] = 9
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		n, addr, e := conn.ReadFrom(buf)
		if e != nil {
			t.Error(e)
			return
		}
		if n != 16 || binary.BigEndian.Uint64(buf[:8]) != 0x41727101980 {
			t.Error("invalid connect request")
			return
		}
		response := make([]byte, 16)
		copy(response[4:8], buf[12:16])
		binary.BigEndian.PutUint64(response[8:], 123)
		conn.WriteTo(response, addr)
		n, addr, e = conn.ReadFrom(buf)
		if e != nil {
			t.Error(e)
			return
		}
		if n != 98 || !bytes.Equal(buf[16:36], hash[:]) || readUint(buf[80:84]) != 2 {
			t.Error("invalid announce request")
			return
		}
		response = make([]byte, 26)
		binary.BigEndian.PutUint32(response[:4], 1)
		copy(response[4:8], buf[12:16])
		binary.BigEndian.PutUint32(response[8:12], 60)
		copy(response[20:24], net.IPv4(127, 0, 0, 1).To4())
		binary.BigEndian.PutUint16(response[24:26], 1234)
		conn.WriteTo(response, addr)
	}()
	u, _ := url.Parse("udp://" + conn.LocalAddr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	peers, interval, e := announceUDP(ctx, u, hash, id, 6881, 0, 0, 100, "started")
	if e != nil || len(peers) != 1 || peers[0].Port != 1234 || interval != time.Minute {
		t.Fatalf("UDP announce: %v %v %v", peers, interval, e)
	}
	<-done
}
func TestDHTLookupAndAnnouncement(t *testing.T) {
	conn, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	done := make(chan struct{})
	var hash [20]byte
	hash[0] = 5
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		n, addr, e := conn.ReadFrom(buf)
		if e != nil {
			t.Error(e)
			return
		}
		v, e := decode(buf[:n])
		if e != nil {
			t.Error(e)
			return
		}
		m := v.(map[string]any)
		args := m["a"].(map[string]any)
		if str(m["q"]) != "get_peers" || str(args["info_hash"]) != string(hash[:]) {
			t.Error("invalid lookup")
			return
		}
		peer := []byte{127, 0, 0, 1, 0x1a, 0xe1}
		var b bytes.Buffer
		bencode.Marshal(&b, map[string]any{"t": m["t"], "y": "r", "r": map[string]any{"id": string(make([]byte, 20)), "token": "token", "values": []string{string(peer)}}})
		conn.WriteTo(b.Bytes(), addr)
		n, _, e = conn.ReadFrom(buf)
		if e != nil {
			t.Error(e)
			return
		}
		v, e = decode(buf[:n])
		if e != nil {
			t.Error(e)
			return
		}
		m = v.(map[string]any)
		args = m["a"].(map[string]any)
		if str(m["q"]) != "announce_peer" || str(args["token"]) != "token" || integer(args["port"]) != 6881 {
			t.Error("invalid DHT announcement")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	peers, e := lookupDHT(ctx, hash, 6881, []string{conn.LocalAddr().String()})
	if e != nil || len(peers) != 1 || peers[0].Port != 6881 {
		t.Fatalf("lookup: %v %v", peers, e)
	}
	<-done
}
func TestMetadataExchange(t *testing.T) {
	meta := fixture(t, []byte("abcd"), 4, false)
	v, _ := decode(meta.RawInfo)
	info := v.(map[string]any)
	info["padding"] = string(bytes.Repeat([]byte("x"), 20000))
	var raw bytes.Buffer
	bencode.Marshal(&raw, info)
	hash := sha1.Sum(raw.Bytes())
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := listener.Accept()
		if e != nil {
			t.Error(e)
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, e = ReadHandshake(conn); e != nil {
			t.Error(e)
			return
		}
		writeFull(conn, NewHandshake(hash, [20]byte{}).Serialize())
		if _, e = ReadMessage(conn); e != nil {
			t.Error(e)
			return
		}
		var b bytes.Buffer
		bencode.Marshal(&b, map[string]any{"m": map[string]any{"ut_metadata": 3}, "metadata_size": raw.Len()})
		writeFull(conn, (&Message{ID: 20, Payload: append([]byte{0}, b.Bytes()...)}).Serialize())
		for piece, a := 0, 0; a < raw.Len(); piece, a = piece+1, a+MaxBlockSize {
			m, e := ReadMessage(conn)
			if e != nil {
				t.Error(e)
				return
			}
			if m == nil || m.ID != 20 || len(m.Payload) < 2 || m.Payload[0] != 3 {
				t.Error("wrong metadata extension ID")
				return
			}
			b.Reset()
			bencode.Marshal(&b, map[string]any{"msg_type": 1, "piece": piece, "total_size": raw.Len()})
			payload := append([]byte{1}, b.Bytes()...)
			payload = append(payload, raw.Bytes()[a:min(a+MaxBlockSize, raw.Len())]...)
			if e = writeFull(conn, (&Message{ID: 20, Payload: payload}).Serialize()); e != nil {
				t.Error(e)
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addr := listener.Addr().(*net.TCPAddr)
	got, e := metadataFromPeer(ctx, Peer{addr.IP, uint16(addr.Port)}, hash, [20]byte{})
	if e != nil || !bytes.Equal(got, raw.Bytes()) {
		t.Fatalf("metadata exchange: %v", e)
	}
	<-done
}

func BenchmarkVerifiedPieceWrite(b *testing.B) {
	data := bytes.Repeat([]byte("x"), 64<<10)
	meta := fixture(b, data, len(data), false)
	s, e := openStorage(meta, b.TempDir(), []int{0})
	if e != nil {
		b.Fatal(e)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if e = s.commit(0, data); e != nil {
			b.Fatal(e)
		}
	}
}
