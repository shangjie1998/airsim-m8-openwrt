package gateway

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// The original AIRSIM-DIAL1 proof remains intact. This additional proof binds
// read-only outcome/state to the authenticated Agent and this request's nonce.
// It is not a new dial command or a recovery by telephone-number matching.
func signedOperationStatus(query url.Values, body []byte, nonce string, key []byte, agentID string) ([]byte, error) {
	op := query.Get("operation_id")
	valid := func(s string) bool { return s != "" && len(s) <= 128 && !strings.ContainsAny(s, "\r\n\x00") }
	if len(query["operation_id"]) != 1 || !valid(op) || !valid(agentID) {
		return nil, errors.New("invalid operation query")
	}
	var result struct {
		Version     int    `json:"version"`
		Operation   string `json:"operation_id"`
		Status      string `json:"status"`
		Canonical   string `json:"android_id,omitempty"`
		Backend     string `json:"backend_id,omitempty"`
		State       string `json:"call_state"`
		EndedAt     string `json:"ended_at,omitempty"`
		Agent       string `json:"agent_id"`
		DialProof   string `json:"operation_proof,omitempty"`
		StatusProof string `json:"status_proof"`
	}
	if json.Unmarshal(body, &result) != nil || result.Version != 1 || result.Operation != op {
		return nil, errors.New("mismatched operation result")
	}
	if strings.ContainsAny(result.EndedAt, "\r\n\x00") || len(result.EndedAt) > 64 {
		return nil, errors.New("invalid terminal time")
	}
	switch result.Status {
	case "confirmed":
		if !valid(result.Canonical) || !valid(result.Backend) {
			return nil, errors.New("missing call identity")
		}
		result.DialProof = Sign(key, "AIRSIM-DIAL1\n"+nonce+"\n"+op+"\n"+result.Canonical+"\n"+result.Backend)
	case "unknown", "rejected":
		if result.Canonical != "" || result.Backend != "" || result.State != "unknown" || result.EndedAt != "" {
			return nil, errors.New("unconfirmed call identity")
		}
	default:
		return nil, errors.New("invalid operation status")
	}
	switch result.State {
	case "unknown", "dialing", "alerting", "active", "held", "ended":
	default:
		return nil, errors.New("invalid call state")
	}
	if (result.State == "ended") != (result.EndedAt != "") {
		return nil, errors.New("invalid terminal outcome")
	}
	result.Agent = agentID
	result.StatusProof = Sign(key, strings.Join([]string{"AIRSIM-OPERATION1", nonce, agentID, op, result.Status, result.Canonical, result.Backend, result.State, result.EndedAt}, "\n"))
	return json.Marshal(result)
}
