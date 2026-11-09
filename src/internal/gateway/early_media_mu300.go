package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type mu300EarlyBackend struct {
	trialMu               sync.Mutex
	trialPath             string
	trialStarted          time.Time
	trialOwner, trialCall string
	client                *http.Client
	capture               func(context.Context) (io.ReadCloser, error)
}

func NewMU300EarlyMediaBackend(capture func(context.Context) (io.ReadCloser, error)) EarlyMediaBackend {
	return &mu300EarlyBackend{client: newEarlyHTTPClient(), capture: capture}
}
func (b *mu300EarlyBackend) Enabled() bool {
	return b.capture != nil
}
func (b *mu300EarlyBackend) Open(ctx context.Context) (io.ReadCloser, error) {
	if !b.Enabled() {
		return nil, errors.New("early source unavailable")
	}
	return b.capture(ctx)
}
func (b *mu300EarlyBackend) Check(ctx context.Context, owner, call string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8575/api/calls/early-media?call_id="+url.QueryEscape(call), nil)
	if err != nil {
		return 410, err
	}
	req.Header.Set("X-AirSIM-Internal-Call-Owner", owner)
	resp, err := b.client.Do(req)
	if err != nil {
		return 410, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == 202 {
			gate := b.trialCheck(owner, call)
			if gate != 200 {
				return gate, nil
			}
		}
		return resp.StatusCode, nil
	}
	var signal struct {
		CallID string `json:"call_id"`
		Native bool   `json:"native_early_media"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&signal); err != nil {
		return 410, err
	}
	if signal.CallID != call || !signal.Native {
		return 410, errors.New("early signal identity mismatch")
	}
	return b.trialCheck(owner, call), nil
}

func newEarlyHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
