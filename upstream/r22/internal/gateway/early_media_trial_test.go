package gateway

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEarlyTrialIsShortLivedSingleOwnerSingleCallAndRevocable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permit.json")
	now := time.Now()
	b := &mu300EarlyBackend{trialPath: path, trialStarted: now.Add(-time.Second)}
	owner := "wlan:fixture-owner"
	if b.trialCheck(owner, "call-one") != 202 {
		t.Fatal("unarmed trial should wait")
	}
	permit := EarlyMediaTrial{1, fmt.Sprintf("%x", sha256.Sum256([]byte(owner))), "call-one", now.Unix(), now.Unix() + 60}
	write := func() {
		raw, _ := json.Marshal(permit)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if b.trialCheck(owner, "call-one") != 200 {
		t.Fatal("explicit scope not armed")
	}
	if b.trialCheck("other-owner", "call-one") != 403 || b.trialCheck(owner, "call-two") != 410 {
		t.Fatal("scope escape")
	}
	permit.CallID = "call-two"
	write()
	if b.trialCheck(owner, "call-two") != 410 {
		t.Fatal("trial reused for replacement call")
	}
	permit.CallID = "call-one"
	permit.ExpiresAt = now.Unix() + 61
	write()
	if b.trialCheck(owner, "call-one") != 410 {
		t.Fatal("long permit accepted")
	}
	permit.ExpiresAt = now.Unix() + 60
	write()
	os.Chmod(path, 0644)
	if b.trialCheck(owner, "call-one") != 410 {
		t.Fatal("public permission accepted")
	}
	os.Chmod(path, 0600)
	os.Remove(path)
	if b.trialCheck(owner, "call-one") != 410 {
		t.Fatal("revoked trial reused")
	}
	b.trialStarted = now.Add(-11 * time.Minute)
	if b.trialCheck(owner, "call-one") != 410 {
		t.Fatal("trial window did not expire")
	}
}
