package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type cloudEarlyFixture struct{ *earlyFixture }

func (b *cloudEarlyFixture) CheckCloud(_ context.Context, q url.Values) (int, error) {
	if q.Get("call_id") != "relay-call" || q.Get("call_uuid") != "relay-uuid" || q.Get("generation") != "7" {
		return 410, nil
	}
	return int(b.code.Load()), nil
}

const cloudFixtureQuery = "?call_id=relay-call&call_uuid=relay-uuid&generation=7"

func TestCloudEarlyStreamBoundReadOnlyAndReleasesOnTransition(t *testing.T) {
	for _, end := range []string{"active", "disconnect"} {
		t.Run(end, func(t *testing.T) {
			g, _, f := earlyTestGateway(t)
			g.SetEarlyMediaBackend(&cloudEarlyFixture{f})
			server := httptest.NewServer(http.HandlerFunc(g.ServeCloudEarlyMedia))
			defer server.Close()
			resp, err := http.Get(server.URL + cloudFixtureQuery)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatal(resp.StatusCode)
			}
			frame := make([]byte, 320)
			if _, err = io.ReadFull(resp.Body, frame); err != nil || string(frame) != strings.Repeat("z", 320) {
				t.Fatal("no real fixture frame", err)
			}
			if f.opens.Load() != 1 {
				t.Fatal("multiple captures")
			}
			busy, err := http.Get(server.URL + cloudFixtureQuery)
			if err != nil {
				t.Fatal(err)
			}
			busy.Body.Close()
			if busy.StatusCode != 409 {
				t.Fatal("concurrent owner", busy.StatusCode)
			}
			g.mu.Lock()
			done := g.earlyStreamDone
			g.mu.Unlock()
			if end == "active" {
				f.code.Store(410)
				go io.Copy(io.Discard, resp.Body)
			} else {
				resp.Body.Close()
			}
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("capture did not release")
			}
			if f.closes.Load() != 1 {
				t.Fatal("capture released incorrectly", f.closes.Load())
			}
			g.mu.Lock()
			owned := g.pcm
			g.mu.Unlock()
			if owned {
				t.Fatal("DSP ownership leaked")
			}
		})
	}
}

func TestCloudEarlyDeniesRemoteMalformedUnsupportedAndPending(t *testing.T) {
	g, _, f := earlyTestGateway(t)
	g.SetEarlyMediaBackend(&cloudEarlyFixture{f})
	for _, tc := range []struct {
		addr, method, query string
		code                int
	}{
		{"192.0.2.1:55", "GET", cloudFixtureQuery, 403},
		{"127.0.0.1:55", "POST", cloudFixtureQuery, 405},
		{"127.0.0.1:55", "GET", cloudFixtureQuery + "&call_id=other", 400},
		{"127.0.0.1:55", "GET", "?call_id=relay-call&call_uuid=relay-uuid&generation=0", 400},
		{"127.0.0.1:55", "GET", "?call_id=relay-call&call_uuid=relay-uuid&generation=07", 400},
		{"127.0.0.1:55", "GET", "?call_id=other&call_uuid=relay-uuid&generation=7", 410},
	} {
		r := httptest.NewRequest(tc.method, "/"+tc.query, nil)
		r.RemoteAddr = tc.addr
		w := httptest.NewRecorder()
		g.ServeCloudEarlyMedia(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s %d != %d", tc.query, w.Code, tc.code)
		}
	}
	for _, code := range []int{202, 403, 410, 501} {
		f.code.Store(int64(code))
		r := httptest.NewRequest("GET", "/"+cloudFixtureQuery, nil)
		r.RemoteAddr = "127.0.0.1:55"
		w := httptest.NewRecorder()
		g.ServeCloudEarlyMedia(w, r)
		if w.Code != code {
			t.Fatal(w.Code, code)
		}
	}
	if f.opens.Load() != 0 {
		t.Fatal("denied request opened capture")
	}
}

func TestNormalEarlyBackendAvailableAndTrialRemainsScoped(t *testing.T) {
	capture := func(context.Context) (io.ReadCloser, error) { t.Fatal("check started capture"); return nil, nil }
	b := NewMU300EarlyMediaBackend(capture).(*mu300EarlyBackend)
	if !b.Enabled() || b.trialCheck("owner", "call") != 200 {
		t.Fatal("ordinary installation still requires lab permit")
	}
	b = NewMU300EarlyMediaTrialBackend(capture, t.TempDir()+"/permit").(*mu300EarlyBackend)
	if b.trialCheck("owner", "call") != 202 {
		t.Fatal("explicit trial lost scope")
	}
	if code, _ := b.CheckCloud(context.Background(), url.Values{}); code != 501 {
		t.Fatal("cloud bypassed trial")
	}
	if NewMU300EarlyMediaBackend(nil).Enabled() {
		t.Fatal("missing source advertised")
	}
}

func TestCloudEarlyInternalEndpointsNeverForwardedFromLAN(t *testing.T) {
	g, key, _ := earlyTestGateway(t)
	for i, path := range []string{"/api/calls/cloud-early-media" + cloudFixtureQuery, "/internal/cloud-early-media" + cloudFixtureQuery} {
		w := signed(g, path, []string{"cloud-private-one", "cloud-private-two"}[i], key)
		if w.Code == 200 || w.Code == 202 {
			t.Fatal("loopback endpoint exposed on WLAN", path, w.Code)
		}
	}
}

type slowCloudCleanup struct {
	*cloudEarlyFixture
	cleanupStarted chan struct{}
	allowCleanup   chan struct{}
}

func (b *slowCloudCleanup) Open(ctx context.Context) (io.ReadCloser, error) {
	source, err := b.cloudEarlyFixture.Open(ctx)
	if err != nil {
		return nil, err
	}
	return &fixtureCapture{ReadCloser: source, closeFn: func() { close(b.cleanupStarted); <-b.allowCleanup }}, nil
}

func TestCloudEarlyReservesAudioUntilCleanupFinishes(t *testing.T) {
	g, _, f := earlyTestGateway(t)
	b := &slowCloudCleanup{cloudEarlyFixture: &cloudEarlyFixture{f}, cleanupStarted: make(chan struct{}), allowCleanup: make(chan struct{})}
	g.SetEarlyMediaBackend(b)
	server := httptest.NewServer(http.HandlerFunc(g.ServeCloudEarlyMedia))
	defer server.Close()
	resp, err := http.Get(server.URL + cloudFixtureQuery)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	g.mu.Lock()
	done := g.earlyStreamDone
	g.mu.Unlock()
	released := false
	defer func() {
		if !released {
			close(b.allowCleanup)
		}
	}()
	resp.Body.Close()
	select {
	case <-b.cleanupStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup not started")
	}
	select {
	case <-done:
		t.Fatal("released reservation before DSP cleanup")
	default:
	}
	g.mu.Lock()
	owned := g.pcm
	g.mu.Unlock()
	if !owned {
		t.Fatal("duplex could race DSP teardown")
	}
	close(b.allowCleanup)
	released = true
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup never released reservation")
	}
	// The next call can use the same gateway after cleanup; no process restart.
	g.SetEarlyMediaBackend(&cloudEarlyFixture{f})
	next, err := http.Get(server.URL + cloudFixtureQuery)
	if err != nil {
		t.Fatal(err)
	}
	// Use an ordinary fixture for a second lifecycle to avoid the single-use test gate.
	// A successful header already proves admission after cleanup.
	if next.StatusCode != 200 {
		t.Fatal(next.StatusCode)
	}
	next.Body.Close()
}
