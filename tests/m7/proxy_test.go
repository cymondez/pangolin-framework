//go:build integration

package m7

// The PostgreSQL wire-fault proxy: the same pattern proven in
// tests/m2/commit_proxy_test.go. It parses PostgreSQL frames and cuts the
// connection either before forwarding COMMIT or after the database's complete
// COMMIT response, while withholding that response from the client: the
// commit result becomes unreadable without any timing-based fault window.

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// commitProxy wraps the wire-fault for tests, matching the M2 helper shape.
func commitProxy(t *testing.T, target string, after bool) (string, func()) {
	return commitProxyRun(target, after)
}

func commitProxyRun(target string, after bool) (string, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", func() {}
	}
	var mu sync.Mutex
	var connections []net.Conn
	var wait sync.WaitGroup
	stop := func() {
		_ = listener.Close()
		mu.Lock()
		for _, connection := range connections {
			_ = connection.Close()
		}
		mu.Unlock()
		wait.Wait()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			client, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			server, dialErr := net.Dial("tcp", target)
			if dialErr != nil {
				_ = client.Close()
				continue
			}
			mu.Lock()
			connections = append(connections, client, server)
			mu.Unlock()
			wait.Add(1)
			go func() {
				defer wait.Done()
				defer client.Close()
				defer server.Close()
				var size [4]byte
				if _, readErr := io.ReadFull(client, size[:]); readErr != nil {
					return
				}
				length := binary.BigEndian.Uint32(size[:])
				if length < 4 || length > 1<<20 {
					return
				}
				startup := make([]byte, length)
				copy(startup, size[:])
				if _, readErr := io.ReadFull(client, startup[4:]); readErr != nil {
					return
				}
				if _, writeErr := server.Write(startup); writeErr != nil {
					return
				}
				var committing atomic.Bool
				serving := make(chan struct{})
				go func() {
					defer close(serving)
					for {
						frame, frameErr := readFrame(server)
						if frameErr != nil {
							return
						}
						if after && committing.Load() {
							// Swallow the commit's wire responses; the
							// transaction was already durably committed.
							if frame[0] == 'Z' {
								return
							}
							continue
						}
						if _, writeErr := client.Write(frame); writeErr != nil {
							return
						}
					}
				}()
				for {
					frame, frameErr := readFrame(client)
					if frameErr != nil {
						break
					}
					committed := frame[0] == 'Q' && strings.EqualFold(strings.TrimRight(string(frame[5:]), "\x00"), "commit")
					if committed {
						committing.Store(true)
						if !after {
							break
						}
					}
					if _, writeErr := server.Write(frame); writeErr != nil {
						break
					}
				}
				_ = server.Close()
				_ = client.Close()
				<-serving
			}()
		}
	}()
	return listener.Addr().String(), stop
}

func readFrame(reader io.Reader) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length < 4 || length > 16<<20 {
		return nil, io.ErrUnexpectedEOF
	}
	frame := make([]byte, length+1)
	copy(frame, header[:])
	if _, err := io.ReadFull(reader, frame[5:]); err != nil {
		return nil, err
	}
	return frame, nil
}
