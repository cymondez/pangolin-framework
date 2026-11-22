//go:build integration

package m2

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// commitProxy parses PostgreSQL frames. It cuts the connection either before
// forwarding COMMIT or after the database's complete COMMIT response, while
// withholding that response from the client. No timing-based fault window.
func commitProxy(t *testing.T, target string, after bool) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var connections []net.Conn
	var wg sync.WaitGroup
	stop := func() {
		_ = listener.Close()
		mu.Lock()
		for _, c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			server, e := net.Dial("tcp", target)
			if e != nil {
				_ = client.Close()
				return
			}
			mu.Lock()
			connections = append(connections, client, server)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer client.Close()
				defer server.Close()
				var size [4]byte
				if _, e := io.ReadFull(client, size[:]); e != nil {
					return
				}
				n := binary.BigEndian.Uint32(size[:])
				if n < 4 || n > 1<<20 {
					return
				}
				startup := make([]byte, n)
				copy(startup, size[:])
				if _, e := io.ReadFull(client, startup[4:]); e != nil {
					return
				}
				if _, e := server.Write(startup); e != nil {
					return
				}
				var committing atomic.Bool
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer client.Close()
					defer server.Close()
					for {
						frame, e := readFrame(server)
						if e != nil {
							return
						}
						if after && committing.Load() {
							if frame[0] == 'Z' {
								return
							}
							continue
						}
						if _, e = client.Write(frame); e != nil {
							return
						}
					}
				}()
				for {
					frame, e := readFrame(client)
					if e != nil {
						break
					}
					commit := frame[0] == 'Q' && strings.EqualFold(strings.TrimRight(string(frame[5:]), "\x00"), "commit")
					if commit {
						committing.Store(true)
						if !after {
							break
						}
					}
					if _, e = server.Write(frame); e != nil {
						break
					}
				}
				_ = server.Close()
				_ = client.Close()
				<-done
			}()
		}
	}()
	return listener.Addr().String(), stop
}
func readFrame(r io.Reader) ([]byte, error) {
	var header [5]byte
	if _, e := io.ReadFull(r, header[:]); e != nil {
		return nil, e
	}
	n := binary.BigEndian.Uint32(header[1:])
	if n < 4 || n > 16<<20 {
		return nil, io.ErrUnexpectedEOF
	}
	frame := make([]byte, n+1)
	copy(frame, header[:])
	_, e := io.ReadFull(r, frame[5:])
	return frame, e
}
