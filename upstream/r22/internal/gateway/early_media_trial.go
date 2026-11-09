package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

// Explicit lab-only window, not production source verification. The local
// operator must separately arm the exact currently owned call, for <=60s.
// No CLI/package/init path enables this by default.
func NewMU300EarlyMediaTrialBackend(capture func(context.Context) (io.ReadCloser, error), path string) EarlyMediaBackend {
	return &mu300EarlyBackend{client: newEarlyHTTPClient(), capture: capture, trialPath: path, trialStarted: time.Now()}
}

// EarlyMediaTrial contains no key, number or audio. OwnerDigest is the persisted
// SHA-256 of the internal owner string, never a substitute for live Agent checks.
type EarlyMediaTrial struct {
	Version     int    `json:"version"`
	OwnerDigest string `json:"owner_digest"`
	CallID      string `json:"call_id"`
	IssuedAt    int64  `json:"issued_at"`
	ExpiresAt   int64  `json:"expires_at"`
}

func readEarlyTrial(path string) (EarlyMediaTrial, error) {
	var p EarlyMediaTrial
	info, err := os.Lstat(path)
	if err != nil {
		return p, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 {
		return p, fmt.Errorf("unsafe trial file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return p, fmt.Errorf("trial owner mismatch")
	}
	f, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return p, fmt.Errorf("trial changed")
	}
	err = json.NewDecoder(io.LimitReader(f, 4097)).Decode(&p)
	return p, err
}
func (b *mu300EarlyBackend) trialCheck(owner, call string) int {
	if b.trialPath == "" {
		return 200
	}
	b.trialMu.Lock()
	defer b.trialMu.Unlock()
	now := time.Now()
	if now.Before(b.trialStarted) || now.Sub(b.trialStarted) > 10*time.Minute {
		return 410
	}
	p, err := readEarlyTrial(b.trialPath)
	if os.IsNotExist(err) && b.trialCall == "" {
		return 202
	}
	if err != nil {
		return 410
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(owner)))
	decoded, e := hex.DecodeString(p.OwnerDigest)
	if p.IssuedAt < b.trialStarted.Unix() && b.trialCall == "" {
		return 202
	}
	if p.Version != 1 || e != nil || len(decoded) != 32 || !earlyIdentity(p.CallID) || p.IssuedAt < b.trialStarted.Unix() || p.IssuedAt > now.Unix() || p.ExpiresAt <= now.Unix() || p.ExpiresAt > p.IssuedAt+60 {
		return 410
	}
	if p.OwnerDigest != digest {
		return 403
	}
	if p.CallID != call {
		return 410
	}
	if b.trialCall != "" && (b.trialCall != call || b.trialOwner != digest) {
		return 410
	}
	b.trialCall, b.trialOwner = call, digest
	return 200
}
