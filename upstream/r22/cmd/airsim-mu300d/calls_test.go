package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type callTransport func(*http.Request) (*http.Response, error)

func (f callTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func callResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestUICallValidationNeverReachesModem(t *testing.T) {
	d := &daemon{client: &http.Client{Transport: callTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid request reached Agent"); return nil, nil })}}
	for _, body := range []string{
		`{"action":"raw_at","number":"ATD123;"}`,
		`{"action":"dial","number":"123;reboot","operation_id":"operation-123"}`,
		`{"action":"dial","number":"*123#","operation_id":"operation-123"}`,
		`{"action":"dial","number":"12345"}`,
		`{"action":"hangup","call_id":""}`,
		`{"action":"answer","call_id":"mu300-fixture","path":"/api/debug"}`,
		`{"action":"answer","call_id":"mu300-fixture"} {}`,
	} {
		w := httptest.NewRecorder()
		d.callAction(w, httptest.NewRequest("POST", "/call/action", strings.NewReader(body)))
		if !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatal("invalid input accepted", body, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	d.callAction(w, httptest.NewRequest("GET", "/call/action", nil))
	if w.Code != 405 {
		t.Fatal("GET accepted")
	}
}

func TestUICallForwardingAndReceipt(t *testing.T) {
	for _, action := range []string{"dial", "answer", "hangup"} {
		t.Run(action, func(t *testing.T) {
			d := &daemon{client: &http.Client{Transport: callTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "http://127.0.0.1:8575/api/calls/"+action || r.Method != "POST" {
					t.Fatal("wrong upstream", r.URL)
				}
				var v map[string]string
				json.NewDecoder(r.Body).Decode(&v)
				if action == "dial" {
					if v["number"] != "+8615550001111" || v["operation_id"] != "luci-operation-123" || len(v) != 2 {
						t.Fatal("bad dial payload", v)
					}
					return callResponse(200, `{"dialing":true,"backend_id":"mu300-fixture","operation_id":"luci-operation-123"}`), nil
				}
				if len(v) != 1 || v["call_id"] != "mu300-selected" {
					t.Fatal("lost scoped call ID", v)
				}
				if action == "answer" {
					return callResponse(200, `{"answered":true}`), nil
				}
				return callResponse(200, `{"hung_up":true}`), nil
			})}}
			body := `{"action":"` + action + `","number":"+86 (155) 5000-1111","call_id":"mu300-selected","operation_id":"operation-123"}`
			w := httptest.NewRecorder()
			d.callAction(w, httptest.NewRequest("POST", "/call/action", strings.NewReader(body)))
			if !strings.Contains(w.Body.String(), `"ok":true`) {
				t.Fatal(w.Body.String())
			}
		})
	}
}

func TestUICallErrorsAndUnknownReceiptNotSuccess(t *testing.T) {
	for _, body := range []string{`{"error":"通话 ID 已失效"}`, `{}`, `{"dialing":true,"backend_id":"mu300-other","operation_id":"wrong-operation"}`, `not-json`} {
		d := &daemon{client: &http.Client{Transport: callTransport(func(*http.Request) (*http.Response, error) { return callResponse(200, body), nil })}}
		w := httptest.NewRecorder()
		d.callAction(w, httptest.NewRequest("POST", "/call/action", strings.NewReader(`{"action":"dial","number":"12345","operation_id":"operation-123"}`)))
		if !strings.Contains(w.Body.String(), `"error"`) || strings.Contains(w.Body.String(), `"ok":true`) {
			t.Fatal("false success", w.Body.String())
		}
	}
}

func TestUICallsSnapshotIsAllowlisted(t *testing.T) {
	d := &daemon{client: &http.Client{Transport: callTransport(func(*http.Request) (*http.Response, error) {
		return callResponse(200, `{"active":{"id":"mu300-two"},"calls":[{"id":"mu300-one","state":"held","number":"12345","private_key":"do-not-leak"},{"id":"mu300-two","state":"active"}],"history":[{"number":"not-needed"}],"last_poll_error":""}`), nil
	})}}
	s := d.callsSnapshot(context.Background())
	b, _ := json.Marshal(s)
	if !s.Available || len(s.Calls) != 2 || strings.Contains(string(b), "do-not-leak") || strings.Contains(string(b), "not-needed") {
		t.Fatal(string(b))
	}
	d.client.Transport = callTransport(func(*http.Request) (*http.Response, error) {
		return callResponse(200, `{"active":null,"last_poll_error":"timeout"}`), nil
	})
	if d.callsSnapshot(context.Background()).Available {
		t.Fatal("poll failure treated as idle")
	}
}

func TestUICallBusyDoesNotForward(t *testing.T) {
	d := &daemon{}
	d.callMu.Lock()
	defer d.callMu.Unlock()
	w := httptest.NewRecorder()
	d.callAction(w, httptest.NewRequest("POST", "/call/action", strings.NewReader(`{"action":"answer","call_id":"mu300-selected"}`)))
	if !strings.Contains(w.Body.String(), "另一项通话操作") {
		t.Fatal(w.Body.String())
	}
}
