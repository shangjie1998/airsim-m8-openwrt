package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// This adapter is internal to the device. Cloud authentication and the exact
// Relay -> local call binding are checked by the Agent, not supplied by a LAN
// client. The public control whitelist deliberately excludes both endpoints.
type cloudEarlyBackend interface {
	EarlyMediaBackend
	CheckCloud(context.Context, url.Values) (int, error)
}

func (b *mu300EarlyBackend) CheckCloud(ctx context.Context, q url.Values) (int, error) {
	// An explicitly selected lab backend must not bypass its per-call permit.
	if b.trialPath != "" {
		return 501, nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8575/api/calls/cloud-early-media?"+q.Encode(), nil)
	if err != nil {
		return 410, err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return 410, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return resp.StatusCode, nil
	}
	var signal struct {
		CallID string `json:"call_id"`
		Native bool   `json:"native_early_media"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&signal); err != nil {
		return 410, err
	}
	if signal.CallID != q.Get("call_id") || !signal.Native {
		return 410, nil
	}
	return 200, nil
}

// ServeCloudEarlyMedia must only be installed on the loopback admin listener.
// There is no upload, duplex fallback, synthetic tone, or call-control command.
func (g *Gateway) ServeCloudEarlyMedia(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		http.Error(w, "loopback only", 403)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method", 405)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	gen, ge := strconv.ParseUint(q.Get("generation"), 10, 64)
	if err != nil || len(q) != 3 || len(q["call_id"]) != 1 || len(q["call_uuid"]) != 1 || len(q["generation"]) != 1 || !earlyIdentity(q.Get("call_id")) || !earlyIdentity(q.Get("call_uuid")) || ge != nil || gen == 0 || strconv.FormatUint(gen, 10) != q.Get("generation") {
		http.Error(w, "identity", 400)
		return
	}
	b, ok := g.earlyBackend.(cloudEarlyBackend)
	if !ok || !b.Enabled() {
		http.Error(w, "unavailable", 501)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	check := func() int {
		bounded, done := context.WithTimeout(ctx, time.Second)
		defer done()
		code, e := b.CheckCloud(bounded, q)
		if e != nil {
			return 410
		}
		switch code {
		case 200, 202, 403, 410, 501:
			return code
		}
		return 410
	}
	if code := check(); code != 200 {
		http.Error(w, http.StatusText(code), code)
		return
	}
	g.mu.Lock()
	if g.pcm || g.closing {
		g.mu.Unlock()
		http.Error(w, "busy", 409)
		return
	}
	g.pcm = true
	done := make(chan struct{})
	g.earlyStreamDone = done
	g.pcmSessions.Add(1)
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.pcm = false
		g.earlyStreamDone = nil
		close(done)
		g.mu.Unlock()
		g.pcmSessions.Done()
	}()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if check() != 200 {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-watchDone }()
	setup := time.AfterFunc(8*time.Second, cancel)
	defer setup.Stop()
	source, e := b.Open(ctx)
	if e != nil {
		http.Error(w, "capture unavailable", 503)
		return
	}
	defer source.Close() // waits for capture/DSP release before ending HTTP body
	sourceDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			source.Close()
		case <-sourceDone:
		}
	}()
	defer close(sourceDone)
	frame := make([]byte, 320)
	if _, e = io.ReadFull(source, frame); e != nil || ctx.Err() != nil || check() != 200 {
		http.Error(w, "source ended", 410)
		return
	}
	setup.Stop()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	rc := http.NewResponseController(w)
	// Override the admin server's finite response deadline for a bounded stream.
	stall := time.AfterFunc(3*time.Second, cancel)
	defer stall.Stop()
	for ctx.Err() == nil {
		if e = rc.SetWriteDeadline(time.Now().Add(2 * time.Second)); e != nil {
			return
		}
		if e = writeEarlyFull(w, frame); e != nil {
			return
		}
		if e = rc.Flush(); e != nil {
			return
		}
		if _, e = io.ReadFull(source, frame); e != nil {
			return
		}
		stall.Reset(3 * time.Second)
	}
}
