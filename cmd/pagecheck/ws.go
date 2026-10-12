package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// wsConn is the client side of a WebSocket (RFC 6455), just what the DevTools protocol needs:
// text messages, masked frames out, fragments, ping and close in.
type wsConn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex // writes
}

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func wsDial(rawURL string) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" || !isLocal(u.Host) {
		return nil, fmt.Errorf("%s: only ws:// to this machine (its own browser)", rawURL)
	}
	c, err := net.DialTimeout("tcp", u.Host, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var k [16]byte
	rand.Read(k[:])
	key := base64.StdEncoding.EncodeToString(k[:])
	fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", u.RequestURI(), u.Host, key)
	r := bufio.NewReader(c)
	res, err := http.ReadResponse(r, &http.Request{Method: "GET"})
	if err != nil {
		c.Close()
		return nil, err
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	if res.StatusCode != http.StatusSwitchingProtocols || res.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		c.Close()
		return nil, fmt.Errorf("websocket handshake: %s", res.Status)
	}
	return &wsConn{c: c, r: r}, nil
}

// frame encodes one final, masked frame.
func frame(op byte, payload []byte) []byte {
	b := []byte{0x80 | op}
	n := len(payload)
	switch {
	case n < 126:
		b = append(b, 0x80|byte(n))
	case n <= 0xffff:
		b = append(b, 0x80|126)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0x80|127)
		b = binary.BigEndian.AppendUint64(b, uint64(n))
	}
	var mask [4]byte
	rand.Read(mask[:])
	b = append(b, mask[:]...)
	for i, x := range payload {
		b = append(b, x^mask[i%4])
	}
	return b
}

func (w *wsConn) write(op byte, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.c.Write(frame(op, payload))
	return err
}

// WriteText sends one text message.
func (w *wsConn) WriteText(b []byte) error { return w.write(0x1, b) }

// ReadMessage gives the next text or binary message (fragments joined; pings answered).
func (w *wsConn) ReadMessage() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var e [2]byte
			if _, err := io.ReadFull(w.r, e[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(e[:]))
		case 127:
			var e [8]byte
			if _, err := io.ReadFull(w.r, e[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(e[:])
		}
		var mask []byte
		if h[1]&0x80 != 0 { // servers do not mask, but a masked frame is still readable
			mask = make([]byte, 4)
			if _, err := io.ReadFull(w.r, mask); err != nil {
				return nil, err
			}
		}
		if n > 256<<20 {
			return nil, fmt.Errorf("websocket: a %d byte frame", n)
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(w.r, p); err != nil {
			return nil, err
		}
		if mask != nil {
			for i := range p {
				p[i] ^= mask[i%4]
			}
		}
		switch op {
		case 0x8: // close
			w.write(0x8, nil)
			return nil, errors.New("websocket: closed")
		case 0x9: // ping
			if err := w.write(0xA, p); err != nil {
				return nil, err
			}
			continue
		case 0xA: // pong
			continue
		}
		msg = append(msg, p...)
		if fin {
			return msg, nil
		}
	}
}

func (w *wsConn) Close() error { return w.c.Close() }

// isLocal says whether a DevTools address is this machine's (pagecheck talks to its own browser).
func isLocal(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return h == "localhost" || strings.HasPrefix(h, "127.") || h == "::1"
}
