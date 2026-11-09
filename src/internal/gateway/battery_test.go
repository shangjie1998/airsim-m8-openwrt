package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBatteryHealthContractAndAuthentication(t *testing.T) {
	now := time.Now().UnixMilli()
	for _, tc := range []struct {
		name, battery string
		want          bool
	}{
		{"zero", fmt.Sprintf(`{"percent":0,"charging":false,"observed_at_ms":%d}`, now), true},
		{"charging", fmt.Sprintf(`{"percent":82,"charging":true,"observed_at_ms":%d}`, now), true},
		{"missing", `null`, false},
		{"malformed", `{"percent":"invalid"}`, false},
		{"not object", `[]`, false},
		{"missing charging", fmt.Sprintf(`{"percent":82,"observed_at_ms":%d}`, now), false},
		{"missing percent", fmt.Sprintf(`{"charging":false,"observed_at_ms":%d}`, now), false},
		{"range", fmt.Sprintf(`{"percent":101,"charging":false,"observed_at_ms":%d}`, now), false},
		{"stale", fmt.Sprintf(`{"percent":82,"charging":false,"observed_at_ms":%d}`, now-300_000), false},
		{"future", fmt.Sprintf(`{"percent":82,"charging":false,"observed_at_ms":%d}`, now+120_000), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := New(filepath.Join(t.TempDir(), "clients.json"), "mu300-test")
			if err != nil {
				t.Fatal(err)
			}
			key := []byte(strings.Repeat("s", 32))
			if err := g.Add(base64.RawURLEncoding.EncodeToString(key)); err != nil {
				t.Fatal(err)
			}
			queries := 0
			g.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				queries++
				if r.Method != "GET" || r.URL.String() != "http://127.0.0.1:8575/api/health" {
					t.Error("unexpected battery IO")
				}
				body := `{"ok":true,"canonical_call_ids":true,"runtime_profile":"mu300-openwrt","battery":` + tc.battery + `}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			w := httptest.NewRecorder()
			g.ServeHTTP(w, httptest.NewRequest("GET", "/v1/health", nil))
			if w.Code != 401 || queries != 0 {
				t.Fatal("unauthenticated battery query")
			}
			w = signed(g, "/v1/health", "battery-test-nonce", key)
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || string(body["canonical_call_ids"]) != "true" || queries != 1 {
				t.Fatal("optional battery broke health")
			}
			if (body["battery"] != nil) != tc.want {
				t.Fatalf("unexpected battery: %s", body["battery"])
			}
			if tc.want {
				var v batteryReading
				if err := json.Unmarshal(body["battery"], &v); err != nil {
					t.Fatal(err)
				}
				if v.ObservedAtMS != now {
					t.Fatal("sample timestamp replaced")
				}
			}
			if signed(g, "/v1/health", "battery-test-nonce", key).Code != 401 {
				t.Fatal("battery request replay allowed")
			}
		})
	}
}

func TestBatteryFreshnessBoundaries(t *testing.T) {
	now := time.UnixMilli(1800000000000)
	for _, tc := range []struct {
		delta int64
		valid bool
	}{{-300000, false}, {-299999, true}, {60000, true}, {60001, false}} {
		raw := json.RawMessage(fmt.Sprintf(`{"percent":50,"charging":false,"observed_at_ms":%d}`, now.UnixMilli()+tc.delta))
		if (validBattery(raw, now) != nil) != tc.valid {
			t.Fatalf("freshness boundary %d", tc.delta)
		}
	}
}
