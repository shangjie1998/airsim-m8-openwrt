package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPR29CallRoutesRequireAuthenticationAndRejectReplay(t *testing.T) {
	for _, path := range []string{"/api/calls/resolve", "/api/calls/history/ack"} {
		t.Run(path, func(t *testing.T) {
			g, err := New(filepath.Join(t.TempDir(), "clients.json"), "mu300-test")
			if err != nil {
				t.Fatal(err)
			}
			key := []byte(strings.Repeat("s", 32))
			if err = g.Add(base64.RawURLEncoding.EncodeToString(key)); err != nil {
				t.Fatal(err)
			}
			forwarded := 0
			body := `{"call_id":"mu300-fixture","call_uuid":"891eae43-89f7-4baa-a21e-e15beecbe584","generation":"7"}`
			if strings.HasSuffix(path, "ack") {
				body = `{"ids":["mu300-fixture"]}`
			}
			g.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				forwarded++
				got, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.URL.Path != path || string(got) != body {
					t.Error("request changed")
				}
				if !strings.HasPrefix(r.Header.Get("X-AirSIM-Internal-Call-Owner"), "wlan:") {
					t.Error("missing authenticated owner")
				}
				return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(`{"error":"stale"}`))}, nil
			})}
			ts := fmt.Sprint(time.Now().Unix())
			nonce := "compat-test-nonce"
			digest := sha256.Sum256([]byte(body))
			mac := hmac.New(sha256.New, key)
			fmt.Fprintf(mac, "POST\n%s\n%x\n%s\n%s", path, digest, ts, nonce)
			sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
			for _, test := range []struct {
				payload string
				auth    bool
				status  int
			}{
				{body, false, 401}, {body + " ", true, 401}, {body, true, 409}, {body, true, 401},
			} {
				r := httptest.NewRequest("POST", path, strings.NewReader(test.payload))
				if test.auth {
					r.Header.Set("X-AirSIM-VoWLAN-Version", "1")
					r.Header.Set("X-AirSIM-VoWLAN-Timestamp", ts)
					r.Header.Set("X-AirSIM-VoWLAN-Nonce", nonce)
					r.Header.Set("X-AirSIM-VoWLAN-Signature", sig)
				}
				w := httptest.NewRecorder()
				g.ServeHTTP(w, r)
				if w.Code != test.status {
					t.Fatalf("got %d want %d", w.Code, test.status)
				}
			}
			if forwarded != 1 {
				t.Fatalf("unauthenticated or replay request forwarded: %d", forwarded)
			}
			for _, method := range []string{"GET", "PUT", "DELETE"} {
				if allowed(method, path) {
					t.Fatalf("unexpected method %s", method)
				}
			}
		})
	}
}
