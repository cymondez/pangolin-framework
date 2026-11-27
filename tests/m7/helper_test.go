//go:build integration

// Acceptance slice for the remaining M7 SignalR scenarios. The tests need the
// pinned real services from deploy/compose/compose.yaml, the demo realtime
// binary and the demo credentials; a missing environment or service is a
// failed acceptance run, never a skip.
package m7

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	natsgo "github.com/nats-io/nats.go"
)

const recordSeparator = 0x1E

func envRequired(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("acceptance environment %s is missing; the M7 gate requires the real pinned services", key)
	}
	return value
}

func freeLoopbackPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func overrideEnv(t *testing.T, overrides map[string]string) []string {
	t.Helper()
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, value := range overrides {
		values[key] = value
	}
	entries := make([]string, 0, len(values))
	for key, value := range values {
		entries = append(entries, key+"="+value)
	}
	return entries
}

type capturedLogs struct {
	mu      sync.Mutex
	content bytes.Buffer
}

func (l *capturedLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.content.Write(p)
}

func (l *capturedLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.content.String()
}

// demoServer owns one demo realtime process; the listener, stream and
// credentials come from the acceptance environment.
type demoServer struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	logs    *capturedLogs
	stopped chan error
	once    sync.Once
	base    string
}

func startDemoServer(t *testing.T, binary, stream string, overrides map[string]string) *demoServer {
	t.Helper()
	port := freeLoopbackPort(t)
	cmd := exec.Command(binary, "--listen", "127.0.0.1:"+port, "--profile", "tenant-only", "--stream", stream, "--stop-on-stdin-eof")
	cmd.Env = overrideEnv(t, overrides)
	logs := &capturedLogs{}
	cmd.Stdout = logs
	cmd.Stderr = logs
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("demo server start: %v", err)
	}
	server := &demoServer{cmd: cmd, stdin: stdin, logs: logs, base: "http://127.0.0.1:" + port}
	server.stopped = make(chan error, 1)
	go func() { server.stopped <- cmd.Wait() }()
	deadline := time.Now().Add(40 * time.Second)
	for {
		if strings.Contains(logs.String(), "LISTEN http://") {
			break
		}
		if time.Now().After(deadline) {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			t.Fatalf("demo server did not become ready\n%s", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Cleanup(func() { _ = server.stop(20 * time.Second) })
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("DEMO SERVER LOG (%s):\n%s", server.base, logs.String())
		}
	})
	return server
}

// stop ends the process through the stdin EOF contract, like the official
// client suite controller does.
func (s *demoServer) stop(timeout time.Duration) error {
	s.once.Do(func() { _ = s.stdin.Close() })
	select {
	case err := <-s.stopped:
		return err
	case <-time.After(timeout):
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
			_ = s.stdin.Close()
			select {
			case <-s.stopped:
			case <-time.After(10 * time.Second):
			}
		}
		return nil
	}
}

func (s *demoServer) kill(t *testing.T) {
	t.Helper()
	s.once.Do(func() { _ = s.stdin.Close() })
	if err := s.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-s.stopped:
		if err == nil {
			t.Fatal("the killed demo server exited successfully")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("killed demo server did not exit")
	}
}

type hubFrame struct {
	Type         int               `json:"type"`
	Target       string            `json:"target"`
	InvocationID string            `json:"invocationId"`
	Error        string            `json:"error"`
	Arguments    []json.RawMessage `json:"arguments"`
	Result       json.RawMessage   `json:"result"`
	Raw          string
}

// hubClient is a minimal SignalR JSON-protocol WebSocket client used to
// negotiate, subscribe, invoke and observe deliveries on a demo node. A single
// reader goroutine feeds a persistent buffer, so negative windows inspect the
// buffer instead of issuing network reads that would close the socket.
type hubClient struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	pending []hubFrame
	arrived chan struct{}
	failed  chan struct{}
	failure error
}

func demoToken(key, user string) string { return key + ":alpha/" + user }

func rawStrings(raw []json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, string(item))
	}
	return out
}

func (f hubFrame) rawOne() []byte {
	if f.Result != nil {
		return f.Result
	}
	if len(f.Arguments) > 0 {
		return f.Arguments[0]
	}
	return nil
}

// isNotification frames carry the stable event id in the first argument.
func (f hubFrame) isNotification() bool {
	return f.Type == 1 && f.Target == "Notification"
}

func connectHub(t *testing.T, base, key, user string) *hubClient {
	t.Helper()
	token := demoToken(key, user)
	request, err := http.NewRequest(http.MethodPost, base+"/hub/negotiate?negotiateVersion=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("negotiate rejected: %d %s", response.StatusCode, body)
	}
	var ticket struct {
		ConnectionToken string `json:"connectionToken"`
	}
	if err := json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/hub?id=" + ticket.ConnectionToken
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &hubClient{conn: conn, arrived: make(chan struct{}, 64), failed: make(chan struct{})}
	client.write(t, `{"protocol":"json","version":1}`)
	// The reader goroutine owns every read and must be live before the
	// handshake reply is awaited.
	go func() {
		defer func() {
			client.mu.Lock()
			if client.failure == nil {
				client.failure = io.EOF
			}
			client.mu.Unlock()
			close(client.failed)
		}()
		for {
			_, data, readErr := conn.Read(context.Background())
			if readErr != nil {
				client.mu.Lock()
				client.failure = readErr
				client.mu.Unlock()
				return
			}
			raw := bytes.TrimSuffix(data, []byte{recordSeparator})
			var frame hubFrame
			if json.Unmarshal(raw, &frame) == nil {
				frame.Raw = string(raw)
			} else {
				frame = hubFrame{Raw: string(raw)}
			}
			if frame.Type == 6 {
				continue
			}
			client.mu.Lock()
			client.pending = append(client.pending, frame)
			client.mu.Unlock()
			select {
			case client.arrived <- struct{}{}:
			default:
			}
		}
	}()
	if hand := client.waitFrame(t, func(frame hubFrame) bool { return frame.Type == 0 }, 5*time.Second, "handshake"); hand.Raw != "{}" {
		t.Fatalf("handshake rejected: %s", hand.Raw)
	}
	// SignalR servers close idle read loops; the client pings like an
	// official one while the reader goroutine owns all reads.
	stopPinger := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopPinger:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":6}`+string(rune(recordSeparator))))
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		close(stopPinger)
		_ = conn.CloseNow()
	})
	return client
}

// take removes and returns the first buffered frame matching pred.
func (c *hubClient) take(pred func(hubFrame) bool) (hubFrame, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index, frame := range c.pending {
		if pred(frame) {
			c.pending = append(c.pending[:index], c.pending[index+1:]...)
			return frame, true
		}
	}
	return hubFrame{}, false
}

// waitFrame drains the buffer and blocks on the reader signal until a frame
// matches pred; every non-matching frame stays buffered for later barriers.
func (c *hubClient) waitFrame(t *testing.T, pred func(hubFrame) bool, wait time.Duration, note string) hubFrame {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		if frame, ok := c.take(pred); ok {
			return frame
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			c.abort()
			select {
			case <-c.failed:
			case <-time.After(5 * time.Second):
			}
			c.mu.Lock()
			failure := c.failure
			c.mu.Unlock()
			t.Fatalf("%s: frame never arrived within %s (reader: %v)", note, wait, failure)
		}
		select {
		case <-c.arrived:
		case <-c.failed:
			c.mu.Lock()
			failure := c.failure
			c.mu.Unlock()
			t.Fatalf("%s: socket closed while waiting (reader: %v)", note, failure)
		case <-time.After(remaining):
		}
	}
}

// assertAbsent proves no buffered frame matches pred. It must follow a
// positive barrier (an awaited completion or notification) so earlier traffic
// settled before the exclusion window.
func (c *hubClient) assertAbsent(t *testing.T, pred func(hubFrame) bool, note string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, frame := range c.pending {
		if pred(frame) {
			t.Fatalf("%s received a forbidden frame: %s", note, frame.Raw)
		}
	}
}

func notifyWithID(frame hubFrame, id string) bool {
	return frame.isNotification() && strings.Contains(strings.Join(rawStrings(frame.Arguments), " "), `"id":"`+id+`"`)
}

func (c *hubClient) awaitCompletion(t *testing.T, invocationID string, wait time.Duration) hubFrame {
	t.Helper()
	return c.waitFrame(t, func(frame hubFrame) bool {
		return frame.Type == 3 && frame.InvocationID == invocationID
	}, wait, "completion "+invocationID)
}

func (c *hubClient) awaitNotification(t *testing.T, id string, wait time.Duration) {
	t.Helper()
	c.waitFrame(t, func(frame hubFrame) bool { return notifyWithID(frame, id) }, wait, "notification "+id)
}

// awaitNotificationErr waits like awaitNotification but returns the failure
// instead of fataling, letting callers enrich diagnostics.
func (c *hubClient) awaitNotificationErr(t *testing.T, id string, wait time.Duration) error {
	t.Helper()
	return c.tryWait(func(frame hubFrame) bool { return notifyWithID(frame, id) }, wait, "notification "+id)
}
func (c *hubClient) tryWait(pred func(hubFrame) bool, wait time.Duration, note string) error {
	deadline := time.Now().Add(wait)
	for {
		if _, ok := c.take(pred); ok {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			c.abort()
			select {
			case <-c.failed:
			case <-time.After(5 * time.Second):
			}
		}
		select {
		case <-c.arrived:
		case <-c.failed:
			c.mu.Lock()
			failure := c.failure
			c.mu.Unlock()
			return fmt.Errorf("%s: socket closed while waiting (reader: %v)", note, failure)
		case <-time.After(remaining):
			return fmt.Errorf("%s: frame never arrived within %s", note, wait)
		}
	}
}

func (c *hubClient) writePayload(payload string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, []byte(payload+string(rune(recordSeparator))))
}

func (c *hubClient) write(t *testing.T, payload string) {
	t.Helper()
	if err := c.writePayload(payload); err != nil {
		t.Fatal(err)
	}
}

func (c *hubClient) invoke(t *testing.T, invocationID, target string, arguments ...any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"type": 1, "invocationId": invocationID, "target": target, "arguments": arguments})
	if err != nil {
		t.Fatal(err)
	}
	c.write(t, string(payload))
}

func (c *hubClient) subscribe(t *testing.T, invocationID string, tenant, kind, id, channel string) {
	t.Helper()
	c.invoke(t, invocationID, "Subscribe", map[string]string{"tenant": tenant, "kind": kind, "id": id, "channel": channel})
}

// readFrame reads one frame, skipping keepalive pings.
func (c *hubClient) abort() { _ = c.conn.CloseNow() }

// waitForStreamGrowth polls the real JetStream stream until the message count
// grows by the expected delta.
func waitForStreamGrowth(t *testing.T, js natsgo.JetStreamContext, stream string, before, delta uint64, wait time.Duration) {
	t.Helper()
	for start := time.Now(); time.Since(start) < wait; {
		if current := streamMessageCount(t, js, stream); current >= before+delta {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("stream %s never grew from %d by %d", stream, before, delta)
}

func invokeNotify(t *testing.T, client *hubClient, invocationID string, channel, userID, data string) string {
	t.Helper()
	destination := map[string]any{"tenant": "alpha", "kind": "tenant", "id": "alpha", "channel": channel}
	if userID != "" {
		destination["userID"] = userID
	}
	client.invoke(t, invocationID, "Notify", destination, map[string]any{"channel": channel, "data": data})
	frame := client.awaitCompletion(t, invocationID, 20*time.Second)
	if frame.Error != "" {
		t.Fatalf("notify %s failed: %s", invocationID, frame.Error)
	}
	var id string
	if len(frame.rawOne()) == 0 {
		t.Fatalf("notify %s returned no id: %s", invocationID, frame.Raw)
	}
	if err := json.Unmarshal(frame.rawOne(), &id); err != nil {
		t.Fatal(err)
	}
	return id
}

// brokerAdmin connects a read-mostly JetStream management handle to inspect
// the actual broker state during the acceptance run.
func brokerAdmin(t *testing.T, natsURL string) (*natsgo.Conn, natsgo.JetStreamContext) {
	t.Helper()
	conn, err := natsgo.Connect(natsURL, natsgo.Timeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	js, err := conn.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	return conn, js
}

func deleteStream(t *testing.T, natsURL, stream string) {
	t.Helper()
	conn, err := natsgo.Connect(natsURL, natsgo.Timeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	js, err := conn.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = js.DeleteStream(stream, natsgo.Context(ctx))
}

// demoEnvironment loads the acceptance service environment.
func demoEnvironment(t *testing.T) (binary, adminDSN, appDSN, workerDSN, natsURL, key string) {
	t.Helper()
	binary = envRequired(t, "PANGOLIN_REALTIME_BINARY")
	adminDSN = envRequired(t, "PANGOLIN_REALTIME_ADMIN_DSN")
	appDSN = envRequired(t, "PANGOLIN_REALTIME_APP_DSN")
	workerDSN = envRequired(t, "PANGOLIN_REALTIME_WORKER_DSN")
	natsURL = envRequired(t, "PANGOLIN_REALTIME_NATS_URL")
	key = envRequired(t, "PANGOLIN_REALTIME_DEMO_KEY")
	return
}

func ensureProvisioned(t *testing.T, binary, adminDSN, natsURL, stream string) {
	t.Helper()
	cmd := exec.Command(binary, "--provision", "--stream", stream)
	cmd.Env = overrideEnv(t, map[string]string{
		"PANGOLIN_REALTIME_ADMIN_DSN": adminDSN,
		"PANGOLIN_REALTIME_NATS_URL":  natsURL,
	})
	var logs strings.Builder
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Run(); err != nil {
		t.Fatalf("demo provisioning failed: %v\n%s", err, logs.String())
	}
}

func uniqueSuffix(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
}

func proxyDSN(t *testing.T, dsn, address string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Host = address
	return parsed.String()
}
