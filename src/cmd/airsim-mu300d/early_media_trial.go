package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"airsim.local/mu300/internal/gateway"
)

// Local operator command only, never a LAN/rpcd route. Does not dial, answer,
// change call state, open PCM, or touch a phone's microphone.
func armEarlyMediaTrial(data string) error {
	return armEarlyMediaTrialWithClient(data, &http.Client{Timeout: 3 * time.Second})
}
func armEarlyMediaTrialWithClient(data string, client *http.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	d := &daemon{client: client}
	calls := d.callsSnapshot(ctx)
	if !calls.Available || len(calls.Calls) != 1 {
		return errors.New("exactly one reliable outgoing test call required")
	}
	c := calls.Calls[0]
	if c.Direction != "outgoing" || (c.State != "dialing" && c.State != "alerting") {
		return errors.New("test call must be outgoing and not connected")
	}
	f, err := os.Open(filepath.Join(data, "mu300-calls", "operations.json"))
	if err != nil {
		return errors.New("owned dial receipt unavailable")
	}
	defer f.Close()
	var journal struct {
		Version int `json:"version"`
		Records map[string]struct {
			CallID  string     `json:"call_id"`
			Owner   string     `json:"owner_digest"`
			Status  string     `json:"status"`
			EndedAt *time.Time `json:"ended_at"`
		} `json:"records"`
	}
	if json.NewDecoder(io.LimitReader(f, 2<<20)).Decode(&journal) != nil || journal.Version != 1 {
		return errors.New("invalid dial receipts")
	}
	owner := ""
	for _, r := range journal.Records {
		if r.CallID == c.ID && r.Status == "confirmed" && r.EndedAt == nil {
			raw, e := hex.DecodeString(r.Owner)
			if e != nil || len(raw) != 32 {
				return errors.New("receipt lacks owner proof")
			}
			if owner != "" && owner != r.Owner {
				return errors.New("ambiguous call owner")
			}
			owner = r.Owner
		}
	}
	if owner == "" || !trialOwnerIsPaired(data, owner) {
		return errors.New("no owned receipt for current call")
	}
	// Check the same exact identity again after journal I/O.
	again := d.callsSnapshot(ctx)
	if !again.Available || len(again.Calls) != 1 || again.Calls[0].ID != c.ID || again.Calls[0].Direction != "outgoing" || (again.Calls[0].State != "dialing" && again.Calls[0].State != "alerting") {
		return errors.New("call changed; not armed")
	}
	now := time.Now().Unix()
	permit := gateway.EarlyMediaTrial{Version: 1, OwnerDigest: owner, CallID: c.ID, IssuedAt: now, ExpiresAt: now + 60}
	temp, err := os.CreateTemp(data, ".early-media-trial-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err = temp.Chmod(0600); err != nil {
		return err
	}
	if err = json.NewEncoder(temp).Encode(permit); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), filepath.Join(data, "early-media-trial.json"))
}

func trialOwnerIsPaired(data, owner string) bool {
	f, err := os.Open(filepath.Join(data, "vowlan-clients.json"))
	if err != nil {
		return false
	}
	defer f.Close()
	var keys []string
	if json.NewDecoder(io.LimitReader(f, 8192)).Decode(&keys) != nil || len(keys) > 16 {
		return false
	}
	for _, encoded := range keys {
		key, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			continue
		}
		internal := fmt.Sprintf("wlan:%x", sha256.Sum256(key))
		clear(key)
		if fmt.Sprintf("%x", sha256.Sum256([]byte(internal))) == owner {
			return true
		}
	}
	return false
}
