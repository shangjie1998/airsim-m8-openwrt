// Package gateway implements the existing AirSIM VoWLAN wire protocol.
// The Agent and unencrypted DSP socket remain bound to loopback.
package gateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Gateway struct {
	mu              sync.Mutex
	path, id        string
	keys            []string
	seen            map[string]int64
	pcm             bool
	earlyBackend    EarlyMediaBackend
	earlyLeases     map[string]*earlyLease
	earlyStreamDone chan struct{}
	pcmSessions     sync.WaitGroup
	closing         bool
	client          *http.Client
	events          map[string]uint64
	lastEvent       string
	lastEventAt     string
	lastUsed        map[string]int64
}

func New(path, id string) (*Gateway, error) {
	g := &Gateway{path: path, id: id, seen: make(map[string]int64), lastUsed: make(map[string]int64), client: &http.Client{Timeout: 75 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return g, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &g.keys) != nil {
		return nil, errors.New("invalid gateway state")
	}
	for _, key := range g.keys {
		if _, err := decode(key); err != nil {
			return nil, err
		}
	}
	return g, nil
}
func decode(s string) ([]byte, error) {
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil || len(b) != 32 {
		return nil, errors.New("invalid gateway key")
	}
	return b, nil
}

// CheckAdd checks a candidate without authorizing it. Pairing uses this before
// Agent registration; a rejected registration must not authorize a local key.
// Pairings have no fixed count limit. Add still validates before durable storage.
func (g *Gateway) CheckAdd(encoded string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.checkAddLocked(encoded)
}
func (g *Gateway) checkAddLocked(encoded string) error {
	if _, err := decode(encoded); err != nil {
		return err
	}
	for _, key := range g.keys {
		if key == encoded {
			return nil
		}
	}
	return nil
}
func (g *Gateway) Add(encoded string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.checkAddLocked(encoded); err != nil {
		return err
	}
	for _, key := range g.keys {
		if key == encoded {
			return nil
		}
	}
	keys := append(append([]string(nil), g.keys...), encoded)
	if err := g.saveLocked(keys); err != nil {
		return err
	}
	g.keys = keys
	return nil
}

// saveLocked replaces the key file atomically. Callers hold g.mu.
func (g *Gateway) saveLocked(keys []string) error {
	b, _ := json.Marshal(keys)
	f, err := os.OpenFile(g.path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(g.path+".tmp", g.path)
}

// ClientInfo contains a SHA-256 fingerprint, never key bytes or a key suffix.
// Activity is authenticated direct traffic observed during this process lifetime.
// Cloud relay traffic does not identify a direct pairing and is not inferred.
const clientOnlineWindow = 120 * time.Second

type ClientInfo struct {
	ID       string `json:"id"`
	LastUsed int64  `json:"last_used,omitempty"`
	Recent   bool   `json:"recent"`
}

// watchOwner derives the public owner fingerprint from a verified pairing key.
// It is a one-way digest; the raw key is never copied into a response or log.
func watchOwner(key []byte) string { return fmt.Sprintf("%x", sha256.Sum256(key)) }

func (g *Gateway) touchClientLocked(owner string, now int64) {
	if g.lastUsed == nil {
		g.lastUsed = make(map[string]int64)
	}
	g.lastUsed[owner] = now
}

// Clients returns the durable paired fingerprints with in-memory activity.
func (g *Gateway) Clients() []ClientInfo {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make([]ClientInfo, 0, len(g.keys))
	now := time.Now().Unix()
	for _, encoded := range g.keys {
		key, _ := decode(encoded)
		owner := watchOwner(key)
		clear(key)
		used := g.lastUsed[owner]
		result = append(result, ClientInfo{owner, used, used > 0 && now >= used && now-used <= int64(clientOnlineWindow/time.Second)})
	}
	return result
}

func (g *Gateway) hasClientLocked(owner string) bool {
	for _, encoded := range g.keys {
		key, _ := decode(encoded)
		match := watchOwner(key) == owner
		clear(key)
		if match {
			return true
		}
	}
	return false
}

// reserveClientPCMLocked admits one owner to the shared PCM slot. A key removed
// after handshake authentication must not open a new audio session.
func (g *Gateway) reserveClientPCMLocked(owner string) bool {
	if g.pcm || g.closing || !g.hasClientLocked(owner) {
		return false
	}
	g.pcm = true
	return true
}

// RemoveClientIdle uses the durable remove path under the admission lock.
// Unknown IDs are harmless (idempotent). No service restart or modem command.
func (g *Gateway) RemoveClientIdle(owner string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pcm || g.earlyStreamDone != nil {
		return errors.New("音频会话进行中，请结束后再移除配对")
	}
	for _, encoded := range g.keys {
		key, _ := decode(encoded)
		match := watchOwner(key) == owner
		clear(key)
		if match {
			return g.removeLocked(encoded)
		}
	}
	return nil
}

func (g *Gateway) removeLocked(encoded string) error {
	parent, e := decode(encoded)
	if e != nil {
		return e
	}
	owner := watchOwner(parent)
	clear(parent)
	keys := make([]string, 0, len(g.keys))
	for _, k := range g.keys {
		if k != encoded {
			keys = append(keys, k)
		}
	}
	if err := g.saveLocked(keys); err != nil {
		return err
	}
	g.keys = keys
	delete(g.lastUsed, owner)
	for k, lease := range g.earlyLeases {
		if lease.owner == "wlan:"+owner {
			delete(g.earlyLeases, k)
		}
	}
	return nil
}
func Sign(key []byte, canonical string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func canonical(method, path string, body []byte, ts, nonce string) string {
	hash := sha256.Sum256(body)
	return fmt.Sprintf("%s\n%s\n%x\n%s\n%s", method, path, hash, ts, nonce)
}
func (g *Gateway) authenticate(method, path string, body []byte, ts, nonce, sig string) []byte {
	stamp, err := strconv.ParseInt(ts, 10, 64)
	now := time.Now().Unix()
	if err != nil || stamp < now-30 || stamp > now+30 || len(nonce) < 7 || len(nonce) > 128 || len(sig) != 43 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for n, expiry := range g.seen {
		if expiry < now {
			delete(g.seen, n)
		}
	}
	if _, ok := g.seen[nonce]; ok {
		return nil
	}
	if len(g.seen) >= 8192 {
		return nil
	}
	for _, encoded := range g.keys {
		key, _ := decode(encoded)
		if hmac.Equal([]byte(Sign(key, canonical(method, path, body, ts, nonce))), []byte(sig)) {
			g.seen[nonce] = now + 61
			g.touchClientLocked(watchOwner(key), now)
			return key
		}
	}
	return nil
}
func allowed(method, path string) bool {
	if method == "PATCH" && path == "/api/sms/settings" {
		return true
	}
	if method == "GET" {
		switch path {
		case "/api/calls/early-media", "/api/sms", "/api/sms/status", "/v1/health", "/api/health", "/api/status", "/api/sim/identity", "/api/events", "/api/calls/status", "/api/calls/events", "/api/calls/operation", "/api/calls/audio/host/config", "/api/push/status":
			return true
		}
	}
	if method == "POST" {
		switch path {
		case "/api/calls/resolve", "/api/calls/history/ack", "/api/sms/send", "/api/sms/ack", "/api/sms/refresh", "/api/calls/dial", "/api/calls/answer", "/api/calls/reject", "/api/calls/hangup", "/api/calls/dtmf", "/api/calls/audio/mute", "/api/calls/audio/host/warmup", "/api/calls/audio/host/register":
			return true
		}
	}
	return false
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.event("control_request")
	w.Header().Set("Cache-Control", "no-store")
	if !allowed(r.Method, r.URL.Path) {
		http.Error(w, "not found", 404)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	if err != nil {
		http.Error(w, "body too large", 413)
		return
	}
	h := r.Header
	nonce := h.Get("X-AirSIM-VoWLAN-Nonce")
	key := g.authenticate(r.Method, r.URL.RequestURI(), body, h.Get("X-AirSIM-VoWLAN-Timestamp"), nonce, h.Get("X-AirSIM-VoWLAN-Signature"))
	if h.Get("X-AirSIM-VoWLAN-Version") != "1" || key == nil {
		g.event("control_auth_rejected")
		http.Error(w, "authentication required", 401)
		return
	}
	defer clear(key)
	g.event("control_authenticated")
	if r.URL.Path == "/api/calls/early-media" {
		g.earlyMediaControl(w, r, key, nonce)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/calls/dial" {
		g.event("dial_request")
	}
	if r.URL.Path == "/v1/health" {
		w.Header().Set("Content-Type", "application/json")
		capable, number, battery, slots := g.agentIdentity(r.Context())
		health := map[string]any{"ok": true, "version": 1, "transport": "vowlan", "agent_id": g.id, "identity_proof": Sign(key, "AIRSIM-HEALTH1\n"+nonce+"\n"+g.id), "canonical_call_ids": capable, "phone_number": number, "agent_kind": "mu300", "device_name": "AirSIM VIPME-M8", "manufacturer": "ZTE", "model": "VIPME-M8"}
		if battery != nil {
			health["battery"] = battery
		}
		// iOS 靠这份带 identity_proof 的设备信息决定用哪张卡；缺了 sim_slots
		// 它就只能退化到单个号码，双卡列表直接消失。
		if len(slots) > 0 {
			health["sim_slots"] = slots
		}
		json.NewEncoder(w).Encode(health)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, "http://127.0.0.1:8575"+r.URL.RequestURI(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// Derive the trusted owner from the verified pairing key. Never copy a
	// caller-provided internal identity header into the loopback request.
	ownerDigest := sha256.Sum256(key)
	req.Header.Set("X-AirSIM-Internal-Call-Owner", fmt.Sprintf("wlan:%x", ownerDigest))
	resp, err := g.client.Do(req)
	if err != nil {
		http.Error(w, "agent unavailable", 502)
		return
	}
	defer resp.Body.Close()
	if r.URL.Path == "/api/calls/operation" && resp.StatusCode == http.StatusOK {
		output, readErr := io.ReadAll(io.LimitReader(resp.Body, 32769))
		if readErr != nil || len(output) > 32768 {
			http.Error(w, "invalid operation receipt", 502)
			return
		}
		output, err = signedOperationStatus(r.URL.Query(), output, nonce, key, g.id)
		if err != nil {
			http.Error(w, "unconfirmed operation receipt", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(output)
		return
	}
	if r.URL.Path == "/api/calls/dial" && resp.StatusCode == http.StatusOK {
		output, err := io.ReadAll(io.LimitReader(resp.Body, 32769))
		if err != nil || len(output) > 32768 {
			http.Error(w, "invalid dial receipt", 502)
			return
		}
		output, err = signedDialReceipt(body, output, nonce, key)
		if err != nil {
			http.Error(w, "unconfirmed dial identity; query call status before retrying", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(output)
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	// SSE events must reach clients immediately, not sit in net/http buffers.
	buf := make([]byte, 4096)
	for {
		n, e := resp.Body.Read(buf)
		if n > 0 {
			if _, err = w.Write(buf[:n]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if e != nil {
			return
		}
	}
}

func (g *Gateway) agentIdentity(parent context.Context) (bool, string, *batteryReading, json.RawMessage) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8575/api/health", nil)
	resp, err := g.client.Do(req)
	if err != nil {
		return false, "", nil, nil
	}
	defer resp.Body.Close()
	var health struct {
		Battery   json.RawMessage `json:"battery"`
		OK        bool            `json:"ok"`
		Canonical bool            `json:"canonical_call_ids"`
		Profile   string          `json:"runtime_profile"`
		Number    string          `json:"phone_number"`
		SIMSlots  json.RawMessage `json:"sim_slots"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&health) != nil || health.Profile != "mu300-openwrt" {
		return false, "", nil, nil
	}
	return health.OK && health.Canonical, health.Number, validBattery(health.Battery, time.Now()), health.SIMSlots
}

func signedDialReceipt(request, response []byte, nonce string, key []byte) ([]byte, error) {
	var asked struct {
		Operation string `json:"operation_id"`
	}
	if err := json.Unmarshal(request, &asked); err != nil {
		return nil, err
	}
	if asked.Operation == "" {
		return response, nil
	}
	var receipt map[string]any
	if err := json.Unmarshal(response, &receipt); err != nil {
		return nil, err
	}
	op, _ := receipt["operation_id"].(string)
	canonicalID, _ := receipt["android_id"].(string)
	backendID, _ := receipt["backend_id"].(string)
	if op != asked.Operation || canonicalID == "" || backendID == "" || strings.ContainsAny(op+canonicalID+backendID, "\r\n\x00") {
		return nil, errors.New("invalid dial identity")
	}
	receipt["operation_proof"] = Sign(key, "AIRSIM-DIAL1\n"+nonce+"\n"+op+"\n"+canonicalID+"\n"+backendID)
	return json.Marshal(receipt)
}
func (g *Gateway) ServePCM(ctx context.Context, c net.Conn) {
	g.mu.Lock()
	if g.closing {
		g.mu.Unlock()
		c.Close()
		return
	}
	g.pcmSessions.Add(1)
	g.mu.Unlock()
	defer g.pcmSessions.Done()
	g.event("pcm_connection")
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReaderSize(c, 1024)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > 1024 {
		g.event("pcm_header_rejected")
		return
	}
	parts := strings.Split(strings.TrimSuffix(string(line), "\n"), " ")
	if len(parts) > 0 && parts[0] == "AIRSIMEM1" {
		g.serveEarlyPCM(ctx, c, reader, parts)
		return
	}
	if len(parts) != 4 || parts[0] != "AIRSIMVWL1" {
		g.event("pcm_header_rejected")
		return
	}
	key := g.authenticate("PCM", "/v1/pcm", nil, parts[1], parts[2], parts[3])
	if key == nil {
		g.event("pcm_auth_rejected")
		return
	}
	owner := watchOwner(key)
	clear(key)
	g.event("pcm_authenticated")
	// Wait for receive-only teardown before opening duplex on the shared DSP.
	g.mu.Lock()
	earlyDone := g.earlyStreamDone
	g.mu.Unlock()
	if earlyDone != nil {
		select {
		case <-earlyDone:
		case <-ctx.Done():
			return
		case <-time.After(18 * time.Second):
			return
		}
	}
	g.mu.Lock()
	if !g.reserveClientPCMLocked(owner) {
		g.mu.Unlock()
		g.event("pcm_busy")
		return
	}
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.pcm = false; g.mu.Unlock() }()
	upstream, err := net.DialTimeout("tcp", "127.0.0.1:7580", 3*time.Second)
	if err != nil {
		g.event("pcm_backend_unavailable")
		return
	}
	defer upstream.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
			upstream.Close()
		case <-done:
		}
	}()
	upstream.SetDeadline(time.Now().Add(18 * time.Second))
	if _, err = io.WriteString(upstream, "AIRSIMPCM1\n"); err != nil {
		g.event("pcm_backend_handshake_failed")
		return
	}
	ack := make([]byte, len("AIRSIMREADY"))
	if _, err = io.ReadFull(upstream, ack); err != nil || string(ack) != "AIRSIMREADY" {
		g.event("pcm_backend_not_ready")
		return
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = c.Write(ack); err != nil {
		g.event("pcm_client_disconnected_before_ready")
		return
	}
	c.SetDeadline(time.Time{})
	upstream.SetDeadline(time.Time{})
	g.event("pcm_ready")
	copied := make(chan struct{})
	go func() { io.Copy(upstream, reader); upstream.Close(); c.Close(); close(copied) }()
	io.Copy(c, upstream)
	upstream.Close()
	c.Close()
	<-copied
	g.event("pcm_closed")
}

// Shutdown follows cancellation of the server context. Stop admission under the
// same lock as Add, then wait for receive-only capture/DSP teardown as well as
// ordinary PCM sessions before the daemon may exit.
func (g *Gateway) Shutdown() {
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()
	g.pcmSessions.Wait()
}
