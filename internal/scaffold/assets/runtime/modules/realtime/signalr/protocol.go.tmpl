package signalr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
)

const recordSeparator = '\x1e'

var errProtocol = errors.New("REALTIME_PROTOCOL_INVALID")

type hubMessage struct {
	Type         int               `json:"type"`
	Target       string            `json:"target,omitempty"`
	InvocationID string            `json:"invocationId,omitempty"`
	Arguments    []json.RawMessage `json:"arguments"`
	StreamIDs    []string          `json:"streamIds,omitempty"`
}

type completion struct {
	Type         int             `json:"type"`
	InvocationID string          `json:"invocationId"`
	Result       json.RawMessage `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
}

func (e *Endpoint) serve(conn *websocket.Conn, session *realtime.Session) {
	defer conn.CloseNow()
	defer session.Close()
	initial, err := e.handshake(conn, session.Context())
	if err != nil {
		return
	}
	ctx := session.Context()
	completions := make(chan completion, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer session.Close()
		e.read(conn, session, initial, completions)
	}()
	defer func() {
		session.Close()
		_ = conn.CloseNow()
		<-readerDone
	}()
	ticker := time.NewTicker(e.options.KeepAlive)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.write(conn, ctx, []byte("{\"type\":6}")); err != nil {
				return
			}
		case result := <-completions:
			raw, err := json.Marshal(result)
			if err != nil {
				return
			}
			if len(raw)+1 > e.options.MaxMessage {
				raw, _ = json.Marshal(completion{Type: 3, InvocationID: result.InvocationID, Error: "REALTIME_RESULT_TOO_LARGE"})
			}
			if err := e.write(conn, ctx, raw); err != nil {
				return
			}
		case item := <-session.Pending():
			var writeErr error
			_ = session.WriteDelivery(item, func(admitted context.Context, target string, notification realtime.Notification) error {
				// Serialize the already-validated notification as a single argument.
				raw, err := json.Marshal(struct {
					Type      int                      `json:"type"`
					Target    string                   `json:"target"`
					Arguments [1]realtime.Notification `json:"arguments"`
				}{1, target, [1]realtime.Notification{notification}})
				if err == nil {
					err = e.write(conn, admitted, raw)
				}
				writeErr = err
				return err
			})
			if writeErr != nil {
				return
			}
		}
	}
}

func (e *Endpoint) handshake(conn *websocket.Conn, ctx context.Context) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, e.options.HandshakeTimeout)
	defer cancel()
	records, err := readRecords(ctx, conn)
	if err != nil {
		return nil, err
	}
	var request struct {
		Protocol string `json:"protocol"`
		Version  int    `json:"version"`
	}
	if err := json.Unmarshal(records[0], &request); err != nil || request.Protocol != "json" || request.Version != 1 {
		_ = e.write(conn, ctx, []byte("{\"error\":\"Only SignalR JSON Hub Protocol version 1 is supported.\"}"))
		return nil, errProtocol
	}
	if err := e.write(conn, ctx, []byte("{}")); err != nil {
		return nil, err
	}
	return records[1:], nil
}

func readRecords(ctx context.Context, conn *websocket.Conn) ([][]byte, error) {
	kind, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if kind != websocket.MessageText || len(data) == 0 || data[len(data)-1] != recordSeparator ||
		!utf8.Valid(data) || bytes.Count(data, []byte{recordSeparator}) > 64 {
		return nil, errProtocol
	}
	return bytes.Split(data[:len(data)-1], []byte{recordSeparator}), nil
}

func (e *Endpoint) write(conn *websocket.Conn, ctx context.Context, raw []byte) error {
	if len(raw)+1 > e.options.MaxMessage {
		return realtime.ErrCapacity
	}
	ctx, cancel := context.WithTimeout(ctx, e.options.WriteTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, append(raw, recordSeparator))
}

type invocation struct {
	id     string
	cancel context.CancelFunc
}

func (e *Endpoint) read(conn *websocket.Conn, session *realtime.Session, records [][]byte, out chan<- completion) {
	ctx := session.Context()
	var lock sync.Mutex
	var active *invocation
	for ctx.Err() == nil {
		for _, raw := range records {
			var message hubMessage
			if err := json.Unmarshal(raw, &message); err != nil || len(message.InvocationID) > 128 {
				return
			}
			switch message.Type {
			case 1:
				if message.Target == "" || len(message.Target) > 128 || message.Arguments == nil || len(message.StreamIDs) != 0 {
					if message.InvocationID != "" && !sendCompletion(ctx, out, completion{Type: 3, InvocationID: message.InvocationID, Error: errProtocol.Error()}) {
						return
					}
					continue
				}
				lock.Lock()
				if active != nil {
					duplicate := message.InvocationID != "" && message.InvocationID == active.id
					lock.Unlock()
					if duplicate {
						return
					}
					if message.InvocationID != "" && !sendCompletion(ctx, out, completion{Type: 3, InvocationID: message.InvocationID, Error: realtime.ErrCapacity.Error()}) {
						return
					}
					continue
				}
				operation, cancel := context.WithCancel(ctx)
				current := &invocation{message.InvocationID, cancel}
				active = current
				lock.Unlock()
				go func(message hubMessage) {
					defer cancel()
					result := invoke(operation, session, message)
					lock.Lock()
					sent := message.InvocationID == "" || sendCompletion(ctx, out, result)
					active = nil
					lock.Unlock()
					if !sent {
						session.Close()
					}
				}(message)
			case 4:
				if message.InvocationID == "" || !sendCompletion(ctx, out, completion{
					Type: 3, InvocationID: message.InvocationID, Error: "REALTIME_STREAMING_UNSUPPORTED",
				}) {
					return
				}
			case 5:
				if message.InvocationID == "" {
					return
				}
				lock.Lock()
				if active != nil && active.id == message.InvocationID {
					active.cancel()
				}
				lock.Unlock()
			case 6:
				// The read deadline is refreshed on every complete message.
			case 7:
				return
			default:
				return
			}
		}
		read, cancel := context.WithTimeout(ctx, e.options.ReadTimeout)
		next, err := readRecords(read, conn)
		cancel()
		if err != nil {
			return
		}
		records = next
	}
}

func sendCompletion(ctx context.Context, out chan<- completion, result completion) bool {
	select {
	case out <- result:
		return true
	case <-ctx.Done():
		return false
	default:
		// A peer that cannot consume responses must not turn them into
		// an unbounded number of waiting goroutines.
		return false
	}
}

func invoke(ctx context.Context, session *realtime.Session, message hubMessage) (result completion) {
	result = completion{Type: 3, InvocationID: message.InvocationID}
	defer func() {
		if recover() != nil {
			result.Result = nil
			result.Error = "REALTIME_HANDLER_FAILED"
		}
	}()
	value, err := session.Invoke(ctx, message.Target, message.Arguments)
	if err == nil && message.InvocationID != "" {
		result.Result, err = json.Marshal(value)
	}
	if err != nil {
		result.Error = publicError(err)
	}
	return result
}

func publicError(err error) string {
	for _, safe := range []error{
		uow.ErrCommitUnknown, realtime.ErrMethod, realtime.ErrInvalid, realtime.ErrCredential, realtime.ErrCapacity,
		realtime.ErrClosed, scope.ErrDenied, scope.ErrKindUnsupported, scope.ErrAuthorityUnavailable,
		context.Canceled, context.DeadlineExceeded,
	} {
		if errors.Is(err, safe) {
			return safe.Error()
		}
	}
	return "REALTIME_HANDLER_FAILED"
}
