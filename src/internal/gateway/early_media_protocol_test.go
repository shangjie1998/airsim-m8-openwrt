package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type earlyFixture struct {
	code     atomic.Int64
	opens    atomic.Int64
	closes   atomic.Int64
	verified bool
	owner    string
}

func (b *earlyFixture) Enabled() bool { return b.verified }
func (b *earlyFixture) Check(_ context.Context, owner, call string) (int, error) {
	if owner != b.owner || call != "call-fixture" {
		return 403, nil
	}
	return int(b.code.Load()), nil
}

type fixtureCapture struct {
	io.ReadCloser
	once    sync.Once
	closeFn func()
}

func (s *fixtureCapture) Close() error {
	s.once.Do(func() { s.ReadCloser.Close(); s.closeFn() })
	return nil
}
func (b *earlyFixture) Open(ctx context.Context) (io.ReadCloser, error) {
	b.opens.Add(1)
	r, w := io.Pipe()
	go func() {
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if _, err := w.Write([]byte(strings.Repeat("z", 320))); err != nil {
				return
			}
		}
	}()
	return &fixtureCapture{ReadCloser: r, closeFn: func() { b.closes.Add(1) }}, nil
}
func earlyTestGateway(t *testing.T) (*Gateway, []byte, *earlyFixture) {
	t.Helper()
	g, err := New(filepath.Join(t.TempDir(), "keys.json"), "agent-fixture")
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("k", 32))
	if err = g.Add(base64.RawURLEncoding.EncodeToString(key)); err != nil {
		t.Fatal(err)
	}
	b := &earlyFixture{verified: true, owner: earlyOwner(key)}
	b.code.Store(200)
	g.SetEarlyMediaBackend(b)
	return g, key, b
}
func grantFor(t *testing.T, g *Gateway, key []byte, nonce string) earlyGrant {
	t.Helper()
	w := signed(g, "/api/calls/early-media?call_id=call-fixture", nonce, key)
	if w.Code != 200 {
		t.Fatalf("grant: %d", w.Code)
	}
	var grant earlyGrant
	if err := json.Unmarshal(w.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	return grant
}
func earlyHello(g earlyGrant, key []byte, nonce string) string {
	binding := strings.Join([]string{g.AgentID, g.CallID, g.SessionID, strconv.FormatInt(g.ExpiresAt, 10), "recvonly", earlyFormat}, "\n")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return fmt.Sprintf("AIRSIMEM1 %s %s %s %s\n", ts, nonce, Sign(key, canonical("PCM", "/v1/pcm/early-media", []byte(binding), ts, nonce)), base64.StdEncoding.EncodeToString([]byte(binding)))
}
func startEarly(t *testing.T, g *Gateway, hello string) (net.Conn, <-chan struct{}) {
	t.Helper()
	s, c := net.Pipe()
	done := make(chan struct{})
	go func() { g.ServePCM(context.Background(), s); close(done) }()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(c, hello); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("early session leaked")
		}
	})
	return c, done
}
func readEarlyReady(t *testing.T, c net.Conn) {
	t.Helper()
	v := make([]byte, len("AIRSIMREADY")+320)
	if _, err := io.ReadFull(c, v); err != nil {
		t.Fatal(err)
	}
	if string(v[:len("AIRSIMREADY")]) != "AIRSIMREADY" || string(v[len("AIRSIMREADY"):]) != strings.Repeat("z", 320) {
		t.Fatal("not real fixture downlink")
	}
}
func awaitEarlyClosed(t *testing.T, c net.Conn, done <-chan struct{}) {
	t.Helper()
	go io.Copy(io.Discard, c)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not stop")
	}
}
func TestEarlyControlProofIdentityAndStatus(t *testing.T) {
	g, key, b := earlyTestGateway(t)
	grant := grantFor(t, g, key, "control-first")
	want := Sign(key, strings.Join([]string{"AIRSIM-EARLY-MEDIA1", "control-first", g.id, "call-fixture", grant.SessionID, strconv.FormatInt(grant.ExpiresAt, 10), "early_media", "recvonly", earlyFormat}, "\n"))
	if grant.Proof != want || grant.Version != 1 || grant.ExpiresAt <= time.Now().Unix() || grant.ExpiresAt > time.Now().Unix()+30 {
		t.Fatal("invalid grant")
	}
	second := grantFor(t, g, key, "control-second")
	if second.SessionID != grant.SessionID {
		t.Fatal("renewal changed session")
	}
	if signed(g, "/api/calls/early-media?call_id=call-fixture", "control-first", key).Code != 401 {
		t.Fatal("nonce replay")
	}
	if signed(g, "/api/calls/early-media?call_id=x&call_id=y", "malformed-query", key).Code != 400 {
		t.Fatal("ambiguous call accepted")
	}
	foreign := []byte(strings.Repeat("f", 32))
	g.Add(base64.RawURLEncoding.EncodeToString(foreign))
	if signed(g, "/api/calls/early-media?call_id=call-fixture", "foreign-control", foreign).Code != 403 {
		t.Fatal("cross-owner grant")
	}
	for i, code := range []int{202, 410, 501} {
		b.code.Store(int64(code))
		if signed(g, "/api/calls/early-media?call_id=call-fixture", fmt.Sprintf("status-nonce-%d", i), key).Code != code {
			t.Fatal("status lost")
		}
	}
	if b.opens.Load() != 0 {
		t.Fatal("control opened capture")
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/calls/early-media?call_id=call-fixture", nil))
	if w.Code != 401 {
		t.Fatal("unauthenticated control")
	}
}
func TestEarlyPCMStopsForActiveTerminalAndUnavailable(t *testing.T) {
	for _, code := range []int{410, 202, 501, 403} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			g, key, b := earlyTestGateway(t)
			grant := grantFor(t, g, key, "stop-control")
			c, done := startEarly(t, g, earlyHello(grant, key, "stop-pcm-nonce"))
			readEarlyReady(t, c)
			b.code.Store(int64(code))
			awaitEarlyClosed(t, c, done)
			if b.opens.Load() != 1 || b.closes.Load() != 1 || g.Diagnostics()["pcm_active"] != false {
				t.Fatal("capture not released")
			}
		})
	}
}
func TestEarlyPCMRejectsUpstreamAndReplay(t *testing.T) {
	g, key, b := earlyTestGateway(t)
	grant := grantFor(t, g, key, "uplink-control")
	hello := earlyHello(grant, key, "uplink-pcm-nonce")
	c, done := startEarly(t, g, hello)
	readEarlyReady(t, c)
	if _, err := c.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	awaitEarlyClosed(t, c, done)
	replay, replayed := startEarly(t, g, hello)
	awaitEarlyClosed(t, replay, replayed)
	if b.opens.Load() != 1 {
		t.Fatal("replayed handshake reopened capture")
	}
}
func TestEarlyPCMRejectsForgedOwnerLeaseAndLegacyBinding(t *testing.T) {
	for _, kind := range []string{"other-owner", "session", "expiry", "call", "agent", "signature", "binding-format"} {
		t.Run(kind, func(t *testing.T) {
			g, key, b := earlyTestGateway(t)
			grant := grantFor(t, g, key, "bad-control")
			signing := key
			switch kind {
			case "other-owner":
				signing = []byte(strings.Repeat("x", 32))
				g.Add(base64.RawURLEncoding.EncodeToString(signing))
			case "session":
				grant.SessionID = "other"
			case "expiry":
				grant.ExpiresAt--
			case "call":
				grant.CallID = "other"
			case "agent":
				grant.AgentID = "other"
			}
			hello := earlyHello(grant, signing, "bad-pcm-nonce")
			if kind == "signature" {
				hello = strings.Replace(hello, "AIRSIMEM1 ", "AIRSIMEM1 0 ", 1)
			}
			if kind == "binding-format" {
				hello = strings.Replace(hello, "AIRSIMEM1 ", "AIRSIMVWL1 ", 1)
			}
			c, done := startEarly(t, g, hello)
			awaitEarlyClosed(t, c, done)
			if b.opens.Load() != 0 {
				t.Fatal("unauthorized capture")
			}
		})
	}
}
func TestEarlyRenewalExtendsRunningStreamAndExpiryClosesIt(t *testing.T) {
	g, key, b := earlyTestGateway(t)
	grant := grantFor(t, g, key, "renew-control")
	// Issue a short-lived fixture grant, preserving the real expiry monitor.
	short := time.Now().Unix() + 2
	g.mu.Lock()
	l := g.earlyLeases[earlyMapKey(b.owner, grant.CallID)]
	l.expires = short
	l.issued[short] = true
	g.mu.Unlock()
	grant.ExpiresAt = short
	c, done := startEarly(t, g, earlyHello(grant, key, "renew-pcm-nonce"))
	readEarlyReady(t, c)
	renewed := grantFor(t, g, key, "renew-control-2")
	if renewed.SessionID != grant.SessionID || renewed.ExpiresAt <= short {
		t.Fatal("not renewed")
	}
	go io.Copy(io.Discard, c)
	// Cross the original issued expiry; the running session uses the new lease.
	timer := time.NewTimer(time.Until(time.Unix(short, 0)) + 150*time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
		t.Fatal("renewed stream expired early")
	case <-timer.C:
	}
	g.mu.Lock()
	l.expires = time.Now().Unix()
	g.mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expired stream survived")
	}
	if b.closes.Load() != 1 {
		t.Fatal("capture leak")
	}
}

func TestEarlyShutdownCancelsCaptureAndClosesAdmission(t *testing.T) {
	g, key, b := earlyTestGateway(t)
	grant := grantFor(t, g, key, "shutdown-control")
	ctx, cancel := context.WithCancel(context.Background())
	s, c := net.Pipe()
	done := make(chan struct{})
	go func() { g.ServePCM(ctx, s); close(done) }()
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	io.WriteString(c, earlyHello(grant, key, "shutdown-pcm"))
	readEarlyReady(t, c)
	cancel()
	stopped := make(chan struct{})
	go func() { g.Shutdown(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown failed to join capture")
	}
	if b.closes.Load() != 1 {
		t.Fatal("shutdown left source")
	}
	s2, c2 := net.Pipe()
	defer c2.Close()
	g.ServePCM(context.Background(), s2)
	if b.opens.Load() != 1 {
		t.Fatal("post-shutdown capture")
	}
}

func TestEarlySecondSessionCannotOverlap(t *testing.T) {
	g, key, b := earlyTestGateway(t)
	grant := grantFor(t, g, key, "overlap-control")
	first, done := startEarly(t, g, earlyHello(grant, key, "overlap-first"))
	readEarlyReady(t, first)
	second, done2 := startEarly(t, g, earlyHello(grant, key, "overlap-second"))
	awaitEarlyClosed(t, second, done2)
	if b.opens.Load() != 1 {
		t.Fatal("overlapping source")
	}
	first.Close()
	awaitEarlyClosed(t, first, done)
}
