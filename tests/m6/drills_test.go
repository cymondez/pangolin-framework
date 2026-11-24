//go:build integration

package m6

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	natsgo "github.com/nats-io/nats.go"
)

const fixtureTokenURLEnv = "PANGOLIN_M1_TOKEN_URL"

type fixtureTokenCall struct {
	method string
	path   string
	body   string
}

func TestM6FixtureTokenUsesSeparateTokenURL(t *testing.T) {
	issuerHits := make(chan struct{}, 1)
	issuerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case issuerHits <- struct{}{}:
		default:
		}
		http.NotFound(w, r)
	}))
	defer issuerServer.Close()

	tokenCalls := make(chan fixtureTokenCall, 1)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read token request: %v", err)
		}
		tokenCalls <- fixtureTokenCall{method: r.Method, path: r.URL.Path, body: string(body)}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fixture-token"}`)
	}))
	defer tokenServer.Close()

	t.Setenv(fixtureTokenURLEnv, tokenServer.URL+"/token")
	status, raw, _ := fixtureTokenRequest(t, issuerServer.URL)
	if status != http.StatusOK || string(raw) != `{"access_token":"fixture-token"}` {
		t.Fatalf("separate fixture token endpoint response = %d %s", status, raw)
	}
	call := <-tokenCalls
	if call.method != http.MethodPost || call.path != "/token" || call.body != `{"subject":"alice","tenant_id":"tenant-a"}` {
		t.Fatalf("fixture token request = %s %s %s", call.method, call.path, call.body)
	}
	select {
	case <-issuerHits:
		t.Fatal("JWT issuer URL was used as the fixture token endpoint")
	default:
	}
}

func TestM6FixtureTokenFallsBackToIssuerTokenPath(t *testing.T) {
	tokenCalls := make(chan fixtureTokenCall, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls <- fixtureTokenCall{method: r.Method, path: r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fallback-token"}`)
	}))
	defer server.Close()
	t.Setenv(fixtureTokenURLEnv, "")

	status, raw, _ := fixtureTokenRequest(t, server.URL)
	if status != http.StatusOK || string(raw) != `{"access_token":"fallback-token"}` {
		t.Fatalf("fallback fixture token response = %d %s", status, raw)
	}
	call := <-tokenCalls
	if call.method != http.MethodPost || call.path != "/token" {
		t.Fatalf("fallback fixture token request = %s %s", call.method, call.path)
	}
}

// TestM6CleanRebuild deploys a unique fresh database with the real framework
// migration set, builds the real example Bookstore API/Worker binaries from
// the module hosting this test (scripts/m6_drills.py points the test at an
// exported clean source tree with a fresh build cache), then operates them
// against the pinned PostgreSQL/NATS/test-IdP fixture: a signed RS256 JWT
// write/read, denial probes, outbox→JetStream→inbox→read-model delivery and
// post-commit reconciliation on brand-new connections.
func TestM6CleanRebuild(t *testing.T) {
	d := openDrillDatabase(t, "rebuild")
	admin := d.adminPool(t)
	deployment := d.sqlDB(t)
	deployStarted := time.Now()
	deployModules(t, deployment, true)
	deploySeconds := time.Since(deployStarted).Seconds()
	installBookstoreSQL(t, admin, "schema.sql")
	serverVersion := d.serverVersion(t)

	root := moduleRoot(t)
	binaries := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	apiBinary := filepath.Join(binaries, "bookstore-api"+suffix)
	workerBinary := filepath.Join(binaries, "bookstore-worker"+suffix)
	apiSeconds, apiBuildLog := buildBinary(t, root, apiBinary, "./examples/bookstore/cmd/api")
	workerSeconds, workerBuildLog := buildBinary(t, root, workerBinary, "./examples/bookstore/cmd/worker")

	stream := "M6_REBUILD_" + strings.ToUpper(rand.Text())
	subject := "pangolin.m6.rebuild.book_created"
	natsURL := requireEnv(t, natsURLEnv)
	natsVersion, deleteStream := provisionStream(t, natsURL, stream, subject)
	defer deleteStream()

	issuer := requireEnv(t, issuerEnv)
	apiPort := freePort(t)
	apiURL := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	apiCommand := exec.Command(apiBinary, "-listen", fmt.Sprintf("127.0.0.1:%d", apiPort), "-run-for", "15s")
	apiCommand.Dir = root
	apiCommand.Env = childEnv(map[string]string{
		"PANGOLIN_DSN":    dsnWithDatabase(t, requireEnv(t, appDSNEnv), d.name),
		"PANGOLIN_ISSUER": issuer,
	})
	apiLog := evidenceFile(t, "api-run.log")
	apiCommand.Stdout = apiLog
	apiCommand.Stderr = apiLog
	apiDone := startProcess(t, apiCommand)

	readyzStarted := time.Now()
	until(t, 10*time.Second, func() bool { return httpStatus(t, "GET", apiURL+"/readyz", nil, nil) == http.StatusNoContent })
	readyzMS := time.Since(readyzStarted).Milliseconds()

	tokenStatus, raw, _ := fixtureTokenRequest(t, issuer)
	if tokenStatus != http.StatusOK {
		t.Fatalf("fixture IdP token failed: %d %s", tokenStatus, raw)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	must(t, json.Unmarshal(raw, &token))
	authorization := map[string]string{"Authorization": "Bearer " + token.AccessToken}

	bookID := rand.Text()
	createStatus, createBody, createElapsed := request(t, "POST", apiURL+"/books", []byte(fmt.Sprintf(`{"id":%q,"title":"M6 clean rebuild drill"}`, bookID)), authorization)
	if createStatus != http.StatusCreated {
		t.Fatalf("rebuild HTTP write failed: %d %s", createStatus, createBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	must(t, json.Unmarshal(createBody, &created))
	if created.ID != bookID {
		t.Fatalf("created book id mismatch: %s != %s", created.ID, bookID)
	}
	// The 2xx proves the transaction already committed: an independent fresh
	// connection must observe the business row and the pending outbox row.
	var booksRows, outboxRows int
	var outboxState string
	d.freshRow(t, fmt.Sprintf("SELECT count(*) FROM pangolin.books WHERE tenant_id='tenant-a' AND id='%s'", bookID), func(row pgx.Row) error { return row.Scan(&booksRows) })
	d.freshRow(t, fmt.Sprintf("SELECT state FROM pangolin.outbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->>'id'='%s'", bookID), func(row pgx.Row) error { return row.Scan(&outboxState) })
	d.freshRow(t, fmt.Sprintf("SELECT count(*) FROM pangolin.outbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->>'id'='%s'", bookID), func(row pgx.Row) error { return row.Scan(&outboxRows) })
	if booksRows != 1 || outboxRows != 1 || outboxState != "pending" {
		t.Fatalf("post-commit reconciliation failed: books=%d outbox=%d state=%s", booksRows, outboxRows, outboxState)
	}

	readStatus, readBody, readElapsed := request(t, "GET", apiURL+"/books/"+bookID, nil, authorization)
	if readStatus != http.StatusOK {
		t.Fatalf("rebuild HTTP read failed: %d %s", readStatus, readBody)
	}

	denials := map[string]bool{}
	if status, _, _ := request(t, "GET", apiURL+"/books/"+bookID, nil, map[string]string{"Authorization": "Bearer forged-token"}); status == http.StatusUnauthorized {
		denials["forged_token_401"] = true
	}
	if status, _, _ := request(t, "GET", apiURL+"/books/"+bookID, nil, map[string]string{"Authorization": "Bearer " + token.AccessToken, "X-Tenant-ID": "tenant-b"}); status == http.StatusForbidden {
		denials["tenant_header_forgery_403"] = true
	}
	if len(denials) != 2 {
		t.Fatalf("rebuilt entry did not enforce denials: %v", denials)
	}

	workerCommand := exec.Command(workerBinary, "-run-for", "8s")
	workerCommand.Dir = root
	workerCommand.Env = childEnv(map[string]string{
		"PANGOLIN_DSN":            dsnWithDatabase(t, requireEnv(t, workerDSNEnv), d.name),
		"PANGOLIN_NATS_URL":       natsURL,
		"PANGOLIN_NATS_STREAM":    stream,
		"PANGOLIN_NATS_SUBJECT":   subject,
		"PANGOLIN_WORKER_TENANTS": "tenant-a",
	})
	workerLog := evidenceFile(t, "worker-run.log")
	workerCommand.Stdout = workerLog
	workerCommand.Stderr = workerLog
	workerDone := startProcess(t, workerCommand)

	var viewsRows, inboxProcessed int
	var deliveryState string
	until(t, 12*time.Second, func() bool {
		d.freshRow(t, fmt.Sprintf("SELECT count(*) FROM pangolin.book_views WHERE tenant_id='tenant-a' AND id='%s'", bookID), func(row pgx.Row) error { return row.Scan(&viewsRows) })
		d.freshRow(t, fmt.Sprintf("SELECT count(*) FROM pangolin.inbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->>'id'='%s' AND processed", bookID), func(row pgx.Row) error { return row.Scan(&inboxProcessed) })
		d.freshRow(t, fmt.Sprintf("SELECT state||':'||attempts FROM pangolin.outbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->>'id'='%s'", bookID), func(row pgx.Row) error { return row.Scan(&deliveryState) })
		return viewsRows == 1 && inboxProcessed == 1 && deliveryState == "sent:1"
	})

	workerExit := waitProcess(t, workerCommand, workerDone, 20*time.Second)
	apiExit := waitProcess(t, apiCommand, apiDone, 25*time.Second)
	if workerExit != 0 || apiExit != 0 {
		t.Fatalf("rebuilt processes failed: worker=%d api=%d", workerExit, apiExit)
	}

	writeEvidence(t, "clean-rebuild.json", map[string]any{
		"database":            d.name,
		"server_version":      serverVersion,
		"nats_server_version": natsVersion,
		"go_version":          runtime.Version(),
		"platform":            map[string]any{"GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH, "num_cpu": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0)},
		"module_root":         root,
		"deploy_seconds":      deploySeconds,
		"build": map[string]any{
			"api_seconds":    apiSeconds,
			"worker_seconds": workerSeconds,
			"api_sha256":     fileSha256(t, apiBinary),
			"worker_sha256":  fileSha256(t, workerBinary),
			"api_log":        filepath.Base(apiBuildLog),
			"worker_log":     filepath.Base(workerBuildLog),
		},
		"api":    map[string]any{"port": apiPort, "run_for": "15s", "exit_code": apiExit, "log": "api-run.log"},
		"worker": map[string]any{"run_for": "8s", "exit_code": workerExit, "log": "worker-run.log"},
		"broker": map[string]any{"url": natsURL, "stream": stream, "subject": subject},
		"http": map[string]any{
			"readyz_ms":     readyzMS,
			"token_status":  tokenStatus,
			"create_status": createStatus,
			"create_ms":     createElapsed.Milliseconds(),
			"read_status":   readStatus,
			"read_ms":       readElapsed.Milliseconds(),
			"book_id":       bookID,
			"jwt_subject":   "alice",
			"jwt_tenant":    "tenant-a",
		},
		"post_commit":       map[string]any{"books_rows": booksRows, "outbox_rows": outboxRows, "outbox_state": outboxState, "payload_id_matches": created.ID == bookID},
		"worker_reconciled": map[string]any{"book_views_rows": viewsRows, "inbox_processed": inboxProcessed, "outbox_state": "sent", "outbox_attempts": 1},
		"denials":           denials,
	})
}

func buildBinary(t *testing.T, root, output, pkg string) (float64, string) {
	t.Helper()
	started := time.Now()
	command := exec.Command("go", "build", "-o", output, pkg)
	command.Dir = root
	command.Env = os.Environ()
	log, err := os.Create(filepath.Join(evidenceDir(t), "build-"+strings.ReplaceAll(pkg, "/", "-")+".log"))
	must(t, err)
	defer func() { _ = log.Close() }()
	command.Stdout = log
	command.Stderr = log
	if err := command.Run(); err != nil {
		raw, _ := os.ReadFile(log.Name())
		t.Fatalf("clean rebuild build failed for %s: %v\n%s", pkg, err, raw)
	}
	return time.Since(started).Seconds(), log.Name()
}

func fileSha256(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	must(t, err)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// provisionStream creates the drill's unique JetStream file stream and returns
// a cleanup that removes it; the drill owns every resource it creates.
func provisionStream(t *testing.T, url, stream, subject string) (string, func()) {
	t.Helper()
	conn, err := natsgo.Connect(url, natsgo.Timeout(3*time.Second), natsgo.NoReconnect())
	must(t, err)
	jetStream, err := conn.JetStream()
	must(t, err)
	_, err = jetStream.AddStream(&natsgo.StreamConfig{Name: stream, Subjects: []string{subject}, Storage: natsgo.FileStorage, MaxBytes: 64 << 20, MaxMsgSize: 1 << 20, MaxAge: 168 * time.Hour})
	must(t, err)
	version := conn.ConnectedServerVersion()
	return version, func() {
		_ = jetStream.DeleteStream(stream)
		conn.Close()
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	must(t, listener.Close())
	return port
}

func evidenceFile(t *testing.T, name string) *os.File {
	t.Helper()
	file, err := os.Create(filepath.Join(evidenceDir(t), name))
	must(t, err)
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func childEnv(extra map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if _, override := extra[key]; !override {
			env = append(env, pair)
		}
	}
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func startProcess(t *testing.T, command *exec.Cmd) <-chan struct{} {
	t.Helper()
	must(t, command.Start())
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = command.Process.Kill()
			<-done
		}
	})
	return done
}

func waitProcess(t *testing.T, command *exec.Cmd, done <-chan struct{}, timeout time.Duration) int {
	t.Helper()
	select {
	case <-done:
		return command.ProcessState.ExitCode()
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-done
		t.Fatalf("process %s did not exit within %s", filepath.Base(command.Path), timeout)
		return -1
	}
}

func until(t *testing.T, timeout time.Duration, probe func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if probe() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("drill condition not reached within %s", timeout)
}

func request(t *testing.T, method, target string, body []byte, headers map[string]string) (int, []byte, time.Duration) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpRequest, err := http.NewRequest(method, target, reader)
	must(t, err)
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		httpRequest.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	started := time.Now()
	response, err := client.Do(httpRequest)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	must(t, err)
	must(t, response.Body.Close())
	return response.StatusCode, raw, time.Since(started)
}

func fixtureTokenRequest(t *testing.T, issuer string) (int, []byte, time.Duration) {
	t.Helper()
	tokenURL := os.Getenv(fixtureTokenURLEnv)
	if tokenURL == "" {
		tokenURL = issuer + "/token"
	}
	return request(t, http.MethodPost, tokenURL, []byte(`{"subject":"alice","tenant_id":"tenant-a"}`), nil)
}

func httpStatus(t *testing.T, method, target string, body []byte, headers map[string]string) int {
	t.Helper()
	probe, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		return 0
	}
	for key, value := range headers {
		probe.Header.Set(key, value)
	}
	response, err := (&http.Client{Timeout: time.Second}).Do(probe)
	if err != nil {
		return 0 // The listener may still be starting during readiness polling.
	}
	defer response.Body.Close()
	return response.StatusCode
}
