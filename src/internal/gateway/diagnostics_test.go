package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRejectedRequestsAreDiagnosableWithoutSensitiveData(t *testing.T) {
	g, err := New(filepath.Join(t.TempDir(), "clients.json"), "test")
	if err != nil {
		t.Fatal(err)
	}
	private := "request-material-must-not-appear"
	r := httptest.NewRequest("POST", "/api/calls/dial", strings.NewReader(private))
	r.Header.Set("X-AirSIM-VoWLAN-Signature", private)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("diagnostics changed authentication enforcement")
	}
	for _, line := range []string{"bad " + private + "\n", "AIRSIMVWL1 0 nonce-test " + private + "\n"} {
		server, client := net.Pipe()
		done := make(chan struct{})
		go func() { g.ServePCM(context.Background(), server); close(done) }()
		client.SetDeadline(time.Now().Add(time.Second))
		if _, err := io.WriteString(client, line); err != nil {
			t.Fatal(err)
		}
		client.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("rejected PCM did not close")
		}
	}
	d := g.Diagnostics()
	events := d["events"].(map[string]uint64)
	for name, expected := range map[string]uint64{"control_request": 1, "control_auth_rejected": 1,
		"pcm_connection": 2, "pcm_header_rejected": 1, "pcm_auth_rejected": 1} {
		if events[name] != expected {
			t.Errorf("%s: got %d, want %d", name, events[name], expected)
		}
	}
	if events["pcm_authenticated"] != 0 || events["dial_request"] != 0 {
		t.Fatal("rejected request counted as authenticated")
	}
	data, _ := json.Marshal(d)
	if strings.Contains(string(data), private) {
		t.Fatal("diagnostics leaked request material")
	}
}

func TestDiagnosticsSnapshotsAreIndependentDuringConcurrentRequests(t *testing.T) {
	g := &Gateway{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				g.event("pcm_connection")
				d := g.Diagnostics()
				d["events"].(map[string]uint64)["pcm_connection"] = 0
			}
		}()
	}
	wg.Wait()
	if g.Diagnostics()["events"].(map[string]uint64)["pcm_connection"] != 800 {
		t.Fatal("snapshot mutation changed live counters")
	}
}
