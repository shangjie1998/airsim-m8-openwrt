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

func TestSMSRoutesRequireSignatureAndForwardExactClientPayload(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/sms", ""},
		{"GET", "/api/sms/status", ""},
		{"POST", "/api/sms/refresh", `{}`},
		{"POST", "/api/sms/ack", `{"ids":["mu300-fixture"]}`},
		{"POST", "/api/sms/send", `{"phone":"15550000001","message":"fixture"}`},
		{"PATCH", "/api/sms/settings", `{"auto_cleanup_me":false}`},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			g, err := New(filepath.Join(t.TempDir(), "keys.json"), "fixture-device")
			if err != nil {
				t.Fatal(err)
			}
			key := []byte(strings.Repeat("s", 32))
			if err := g.Add(base64.RawURLEncoding.EncodeToString(key)); err != nil {
				t.Fatal(err)
			}
			calls := 0
			g.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				b, _ := io.ReadAll(r.Body)
				if r.URL.String() != "http://127.0.0.1:8575"+tc.path || r.Method != tc.method || string(b) != tc.body {
					t.Fatal("SMS request changed")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"fixture":true}`))}, nil
			})}
			req := func(signed, tampered bool) *http.Request {
				body := tc.body
				if tampered {
					body += " "
				}
				r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(body))
				if signed {
					ts := fmt.Sprint(time.Now().Unix())
					nonce := "sms-fixture-nonce"
					digest := sha256.Sum256([]byte(tc.body))
					mac := hmac.New(sha256.New, key)
					fmt.Fprintf(mac, "%s\n%s\n%x\n%s\n%s", tc.method, tc.path, digest, ts, nonce)
					r.Header.Set("X-AirSIM-VoWLAN-Version", "1")
					r.Header.Set("X-AirSIM-VoWLAN-Timestamp", ts)
					r.Header.Set("X-AirSIM-VoWLAN-Nonce", nonce)
					r.Header.Set("X-AirSIM-VoWLAN-Signature", base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
				}
				return r
			}
			for _, r := range []*http.Request{req(false, false), req(true, true)} {
				w := httptest.NewRecorder()
				g.ServeHTTP(w, r)
				if w.Code != 401 || calls != 0 {
					t.Fatal("unauthenticated/tampered SMS reached Agent")
				}
			}
			w := httptest.NewRecorder()
			g.ServeHTTP(w, req(true, false))
			if w.Code != 200 || calls != 1 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("signed SMS unavailable")
			}
			w = httptest.NewRecorder()
			g.ServeHTTP(w, req(true, false))
			if w.Code != 401 || calls != 1 {
				t.Fatal("SMS replay reached Agent")
			}
		})
	}
}

func TestSMSDestructiveAndUnknownRoutesRemainClosed(t *testing.T) {
	for _, path := range []string{"/api/sms/clear-module", "/api/sms/delete", "/api/sms/raw", "/api/at"} {
		for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
			if allowed(method, path) {
				t.Fatalf("exposed %s %s", method, path)
			}
		}
	}
}
