package main

import (
	"airsim.local/mu300/internal/gateway"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type earlyTrialTransport func(*http.Request) (*http.Response, error)

func (f earlyTrialTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTrialArmOnlyWritesPermitForExactOwnedOutgoingCall(t *testing.T) {
	for _, kind := range []string{"valid", "active", "incoming", "changed", "no-owner", "unpaired", "multiple"} {
		t.Run(kind, func(t *testing.T) {
			data := t.TempDir()
			os.Mkdir(filepath.Join(data, "mu300-calls"), 0700)
			key := []byte(strings.Repeat("k", 32))
			owner := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("wlan:%x", sha256.Sum256(key)))))
			keys, _ := json.Marshal([]string{base64.RawURLEncoding.EncodeToString(key)})
			os.WriteFile(filepath.Join(data, "vowlan-clients.json"), keys, 0600)
			if kind == "unpaired" {
				owner = strings.Repeat("a", 64)
			}
			if kind == "no-owner" {
				owner = ""
			}
			record := map[string]any{"version": 1, "records": map[string]any{"fixture": map[string]any{"status": "confirmed", "call_id": "mu300-fixture", "owner_digest": owner}}}
			raw, _ := json.Marshal(record)
			os.WriteFile(filepath.Join(data, "mu300-calls", "operations.json"), raw, 0600)
			requests := 0
			client := &http.Client{Transport: earlyTrialTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.Path != "/api/calls/status" {
					t.Error("trial sent control")
				}
				requests++
				id, state, direction := "mu300-fixture", "alerting", "outgoing"
				if kind == "active" {
					state = "active"
				}
				if kind == "incoming" {
					direction = "incoming"
				}
				if kind == "changed" && requests == 2 {
					id = "mu300-other"
				}
				calls := []uiCall{{ID: id, State: state, Direction: direction}}
				if kind == "multiple" {
					calls = append(calls, uiCall{ID: "mu300-other", State: "held"})
				}
				body, _ := json.Marshal(map[string]any{"active": calls[0], "calls": calls, "last_poll_error": ""})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			err := armEarlyMediaTrialWithClient(data, client)
			permitPath := filepath.Join(data, "early-media-trial.json")
			if kind != "valid" {
				if err == nil {
					t.Fatal("unsafe call armed")
				}
				if _, e := os.Stat(permitPath); !os.IsNotExist(e) {
					t.Fatal("unexpected permit")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err = os.ReadFile(permitPath)
			if err != nil {
				t.Fatal(err)
			}
			var permit gateway.EarlyMediaTrial
			json.Unmarshal(raw, &permit)
			if permit.CallID != "mu300-fixture" || permit.OwnerDigest != owner || permit.ExpiresAt-permit.IssuedAt != 60 || permit.ExpiresAt <= time.Now().Unix() {
				t.Fatal("bad scope")
			}
			info, _ := os.Stat(permitPath)
			if info.Mode().Perm() != 0600 {
				t.Fatal("permit exposed")
			}
		})
	}
}
