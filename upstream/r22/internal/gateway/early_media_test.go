package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// U30 has no verified receive-only early-media source yet. A new client must
// fall back to its local waiting tone, never enter the legacy duplex bridge.
func TestUnverifiedEarlyMediaDoesNotDowngradeToDuplex(t *testing.T) {
	g, _ := New(filepath.Join(t.TempDir(), "keys.json"), "mu300-fixture")
	key := []byte(strings.Repeat("e", 32))
	if err := g.Add(base64.RawURLEncoding.EncodeToString(key)); err != nil {
		t.Fatal(err)
	}
	if w := signed(g, "/api/calls/early-media?call_id=mu300-fixture-call", "early-control-nonce", key); w.Code != 501 {
		t.Fatal("unverified capability advertised", w.Code)
	}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { g.ServePCM(context.Background(), server); close(done) }()
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	ts := fmt.Sprint(time.Now().Unix())
	expiry := fmt.Sprint(time.Now().Add(25 * time.Second).Unix())
	nonce := "early-pcm-nonce"
	binding := []byte(strings.Join([]string{"mu300-fixture", "mu300-fixture-call", "session", expiry, "recvonly", "s16le-8000-mono"}, "\n"))
	sig := Sign(key, canonical("PCM", "/v1/pcm/early-media", binding, ts, nonce))
	fmt.Fprintf(client, "AIRSIMEM1 %s %s %s %s\n", ts, nonce, sig, base64.StdEncoding.EncodeToString(binding))
	b := make([]byte, 32)
	n, e := client.Read(b)
	if n != 0 || e != io.EOF {
		t.Fatal("unsupported early session received audio/ACK", n, e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unsupported session leaked")
	}
	d := g.Diagnostics()
	events := d["events"].(map[string]uint64)
	if d["pcm_active"] != false || events["pcm_authenticated"] != 0 || events["pcm_ready"] != 0 {
		t.Fatal("early handshake entered duplex backend")
	}
}
