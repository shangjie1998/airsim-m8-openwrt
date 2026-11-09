package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type uiCall struct {
	ID        string `json:"id"`
	Number    string `json:"number,omitempty"`
	State     string `json:"state"`
	Direction string `json:"direction"`
}
type uiCalls struct {
	Available bool     `json:"available"`
	Calls     []uiCall `json:"calls"`
	Error     string   `json:"error,omitempty"`
}

func (d *daemon) callsSnapshot(parent context.Context) uiCalls {
	result := uiCalls{Calls: []uiCall{}}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8575/api/calls/status", nil)
	resp, err := d.client.Do(req)
	if err != nil {
		result.Error = "无法读取通话状态，已暂停拨号操作"
		return result
	}
	defer resp.Body.Close()
	var state struct {
		Active    json.RawMessage `json:"active"`
		Calls     []uiCall        `json:"calls"`
		LastError string          `json:"last_poll_error"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&state) != nil || len(state.Active) == 0 {
		result.Error = "通话状态响应无效"
		return result
	}
	if state.LastError != "" {
		result.Error = "基带通话状态暂不可用，请稍后重试"
		return result
	}
	if len(state.Calls) > 0 {
		result.Calls = state.Calls
	} else if string(state.Active) != "null" {
		var active uiCall
		if json.Unmarshal(state.Active, &active) != nil {
			result.Error = "当前通话响应无效"
			return result
		}
		result.Calls = []uiCall{active}
	}
	for _, call := range result.Calls {
		if !uiCallID.MatchString(call.ID) {
			result.Calls = []uiCall{}
			result.Error = "通话身份无效，已暂停操作"
			return result
		}
	}
	result.Available = true
	return result
}

var uiDialNumber = regexp.MustCompile(`^\+?[0-9]{3,20}$`)
var uiCallID = regexp.MustCompile(`^mu300-[A-Za-z0-9-]{1,100}$`)
var uiOperationID = regexp.MustCompile(`^[A-Za-z0-9-]{8,100}$`)

// This endpoint is loopback-only and exposed through authenticated rpcd write
// ACLs, never the public pairing/VoWLAN mux. No raw AT or arbitrary paths.
func (d *daemon) callAction(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Action      string `json:"action"`
		Number      string `json:"number"`
		CallID      string `json:"call_id"`
		OperationID string `json:"operation_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		write(w, 200, map[string]any{"error": "通话请求格式无效"})
		return
	}
	payload := map[string]string{}
	switch input.Action {
	case "dial":
		input.Number = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(input.Number))
		if !uiDialNumber.MatchString(input.Number) || !uiOperationID.MatchString(input.OperationID) {
			write(w, 200, map[string]any{"error": "请输入有效电话号码；拨号请求必须包含独立操作编号"})
			return
		}
		payload["number"], payload["operation_id"] = input.Number, "luci-"+input.OperationID
	case "answer", "hangup":
		if !uiCallID.MatchString(input.CallID) {
			write(w, 200, map[string]any{"error": "请先选择要操作的通话"})
			return
		}
		payload["call_id"] = input.CallID
	default:
		write(w, 200, map[string]any{"error": "不支持的通话操作"})
		return
	}
	if !d.callMu.TryLock() {
		write(w, 200, map[string]any{"error": "另一项通话操作正在处理中，请等待结果"})
		return
	}
	defer d.callMu.Unlock()
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:8575/api/calls/"+input.Action, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := *d.client
	client.Timeout = 35 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		write(w, 200, map[string]any{"error": "通话操作结果未确认，请查看实时状态，勿重复拨号", "uncertain": true})
		return
	}
	defer resp.Body.Close()
	var result map[string]json.RawMessage
	if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result) != nil {
		write(w, 200, map[string]any{"error": "通话操作响应无效，请先查询状态", "uncertain": true})
		return
	}
	var detail string
	_ = json.Unmarshal(result["error"], &detail)
	if resp.StatusCode != 200 || detail != "" {
		if detail == "" {
			detail = "Agent 未确认通话操作，请查看实时状态"
		}
		write(w, 200, map[string]any{"error": detail})
		return
	}
	var confirmed bool
	field := map[string]string{"dial": "dialing", "answer": "answered", "hangup": "hung_up"}[input.Action]
	_ = json.Unmarshal(result[field], &confirmed)
	var id string
	_ = json.Unmarshal(result["backend_id"], &id)
	if input.Action == "dial" {
		var op string
		_ = json.Unmarshal(result["operation_id"], &op)
		confirmed = confirmed && uiCallID.MatchString(id) && op == payload["operation_id"]
	}
	if !confirmed {
		write(w, 200, map[string]any{"error": "未收到有效操作回执，请先查询状态", "uncertain": true})
		return
	}
	write(w, 200, map[string]any{"ok": true, "call_id": id})
}
