package gateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOperationOwnerCannotBeForgedAndStatusIsSigned(t *testing.T) {
	g, _ := New(filepath.Join(t.TempDir(), "keys.json"), "mu300-fixture")
	key := []byte(strings.Repeat("k", 32))
	g.Add(base64.RawURLEncoding.EncodeToString(key))
	calls := 0
	g.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		owner := sha256.Sum256(key)
		if r.Method != "GET" || r.URL.Path != "/api/calls/operation" || r.Header.Get("X-AirSIM-Internal-Call-Owner") != fmt.Sprintf("wlan:%x", owner) {
			t.Fatal("untrusted owner or mutated query")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":1,"operation_id":"intent","status":"confirmed","android_id":"mu300-call","backend_id":"mu300-call","call_state":"ended","ended_at":"2026-11-03T00:00:00Z"}`))}, nil
	})}
	path := "/api/calls/operation?operation_id=intent"
	nonce := "query-nonce"
	ts := fmt.Sprint(time.Now().Unix())
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("X-AirSIM-Internal-Call-Owner", "wlan:forged")
	r.Header.Set("X-AirSIM-VoWLAN-Version", "1")
	r.Header.Set("X-AirSIM-VoWLAN-Timestamp", ts)
	r.Header.Set("X-AirSIM-VoWLAN-Nonce", nonce)
	r.Header.Set("X-AirSIM-VoWLAN-Signature", Sign(key, canonical("GET", path, nil, ts, nonce)))
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got["agent_id"] != "mu300-fixture" || got["operation_proof"] != Sign(key, "AIRSIM-DIAL1\nquery-nonce\nintent\nmu300-call\nmu300-call") || got["status_proof"] != Sign(key, "AIRSIM-OPERATION1\nquery-nonce\nmu300-fixture\nintent\nconfirmed\nmu300-call\nmu300-call\nended\n2026-11-03T00:00:00Z") {
		t.Fatal("status proof mismatch", w.Code)
	}
	if calls != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("query not private/read-only")
	}
	w = httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 401 || calls != 1 {
		t.Fatal("replay touched backend")
	}
	if allowed("POST", "/api/calls/operation") {
		t.Fatal("mutation allowed")
	}
}
func TestOperationStatusRejectsUnboundOrInvalidResults(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	query := url.Values{"operation_id": {"intent"}}
	invalid := []string{
		`{"version":1,"operation_id":"other","status":"unknown","call_state":"unknown"}`,
		`{"version":1,"operation_id":"intent","status":"confirmed","call_state":"active"}`,
		`{"version":1,"operation_id":"intent","status":"unknown","call_state":"active","android_id":"other","backend_id":"other"}`,
		`{"version":1,"operation_id":"intent","status":"confirmed","call_state":"ended","android_id":"one","backend_id":"one"}`,
		`{"version":1,"operation_id":"intent","status":"confirmed","call_state":"active","android_id":"one\nforged","backend_id":"one"}`,
	}
	for _, body := range invalid {
		if _, e := signedOperationStatus(query, []byte(body), "nonce", key, "agent"); e == nil {
			t.Fatal("invalid status signed")
		}
	}
	body := []byte(`{"version":1,"operation_id":"intent","status":"unknown","call_state":"unknown"}`)
	out, e := signedOperationStatus(query, body, "nonce", key, "agent")
	if e != nil || strings.Contains(string(out), "operation_proof") {
		t.Fatal("unknown status fabricated a dial receipt", e)
	}
	query.Add("operation_id", "other")
	if _, e = signedOperationStatus(query, body, "nonce", key, "agent"); e == nil {
		t.Fatal("ambiguous query signed")
	}
}
func TestOperationOwnerScopedAcrossPairingKeys(t *testing.T) {
	g, _ := New(filepath.Join(t.TempDir(), "keys.json"), "agent")
	var owners []string
	g.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		owners = append(owners, r.Header.Get("X-AirSIM-Internal-Call-Owner"))
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"error":"not found"}`))}, nil
	})}
	for i := 0; i < 2; i++ {
		key := sha256.Sum256([]byte(fmt.Sprint(i)))
		g.Add(base64.RawURLEncoding.EncodeToString(key[:]))
		w := signed(g, "/api/calls/operation?operation_id=same", fmt.Sprintf("nonce-owner-%d", i), key[:])
		if w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
	if len(owners) != 2 || owners[0] == owners[1] {
		t.Fatal("paired identities merged")
	}
}
