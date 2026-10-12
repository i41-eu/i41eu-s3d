package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
)

// Messages of every length class (7-bit, 16-bit, 64-bit), sent in two fragments after a ping,
// come back whole; the ping is answered, not returned.
func TestFrames(t *testing.T) {
	for _, n := range []int{0, 5, 125, 126, 70000} {
		a, b := net.Pipe()
		w := &wsConn{c: b, r: bufio.NewReader(b)}
		msg := bytes.Repeat([]byte{'x'}, n)
		pong := make(chan []byte, 1)
		go func() {
			a.Write(frame(0x9, []byte("ping")))
			p := make([]byte, 2+4+4) // the pong: header, mask, "ping"
			io.ReadFull(a, p)
			pong <- p
			first := frame(0x1, msg[:n/2])
			first[0] &^= 0x80 // not final
			a.Write(first)
			a.Write(frame(0x0, msg[n/2:]))
		}()
		got, err := w.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, msg) {
			t.Errorf("%d bytes: got %d", n, len(got))
		}
		if p := <-pong; p[0] != 0x8A {
			t.Errorf("no pong: %x", p[0])
		}
		a.Close()
		b.Close()
	}
}
