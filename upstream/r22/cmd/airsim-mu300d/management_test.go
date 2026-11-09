package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsValidation(t *testing.T) {
	for _, item := range []struct {
		input string
		ok    bool
	}{
		{`{}`, true},
		{`{"mode":"local","relay_url":"https://relay.example/base/","interface":"br-lan","pair_port":18580,"control_port":18581,"pcm_port":18582}`, true},
		{`{"mode":"anything"}`, false}, {`{"control_port":8580}`, false},
		{`{"pcm_port":7580}`, false}, {`{"pair_port":8575}`, false}, {`{"pair_port":80}`, false},
		{`{"pair_port":0}`, false}, {`{"pair_port":65536}`, false}, {`{"pair_port":1.5}`, false},
		{`{"interface":"br-lan;reboot"}`, false}, {`{"secret":"not-supported"}`, false},
		{`{"relay_url":"http://relay.example"}`, false}, {`{"relay_url":"https://user:pass@relay.example"}`, false},
		{`{"relay_url":"https://relay.example?token=secret"}`, false}, {`{"relay_url":"https://relay.example#secret"}`, false},
		{`{} {}`, false}, {`null`, false},
	} {
		_, e := decodeSettings(strings.NewReader(item.input))
		if (e == nil) != item.ok {
			t.Errorf("%s: %v", item.input, e)
		}
	}
}

func TestSettingsAtomicStorageAndRuntimeLock(t *testing.T) {
	dir := t.TempDir()
	s, e := loadSettings(dir)
	if e != nil || s != defaultSettings() {
		t.Fatal("defaults failed", e)
	}
	s.Mode = "local"
	s.ControlPort = 18581
	if e := writeSettingsFile(dir, s); e != nil {
		t.Fatal(e)
	}
	loaded, e := loadSettings(dir)
	if e != nil || loaded != s {
		t.Fatal("roundtrip", e)
	}
	info, _ := os.Stat(filepath.Join(dir, "management.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions")
	}
	a, e := managementLock(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	b, e := managementLock(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	if lock, e := managementLock(dir, true); e == nil {
		lock.Close()
		t.Fatal("save allowed while running")
	}
	a.Close()
	b.Close()
	write, e := managementLock(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	defer write.Close()
	if lock, e := managementLock(dir, false); e == nil {
		lock.Close()
		t.Fatal("runtime started during save")
	}
}

func TestDiagnosticAllowlistNeverExportsPrivateFields(t *testing.T) {
	s := map[string]any{}
	if e := json.Unmarshal([]byte(`{"pairing":{"code":"123456"},"lan_ip":"192.168.1.10","agent_id":"private-id","sim":{"phone_number":"15550001111","imsi":"private-imsi","sim_inserted":true},"audio":{"active":false,"last_error":"secret pcm"},"relay":{"configured":true,"relay_url":"https://secret.example","device_secret":"private-key","last_error":"token=secret"},"calls":{"available":true,"calls":[{"number":"15550002222"}]},"sms":"private-message"}`), &s); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(diagnosticReport(s, defaultSettings()))
	for _, forbidden := range []string{"123456", "192.168", "private-", "secret", "155500", "phone_number", "relay_url", "last_error"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatalf("leaked %s: %s", forbidden, b)
		}
	}
	if !diagnosticReport(s, defaultSettings())["relay_error_present"].(bool) {
		t.Fatal("error flag missing")
	}
	if !diagnosticReport(nil, defaultSettings())["call_active_or_unknown"].(bool) {
		t.Fatal("offline reported idle")
	}
}

func TestUpdateSelection(t *testing.T) {
	var releases []githubRelease
	if e := json.Unmarshal([]byte(`[
	 {"tag_name":"v0.1.0-r9","assets":[{"name":"luci-app-airsim-0.1.0-r9.apk"}]},
	 {"tag_name":"v0.1.0-r13","prerelease":true,"assets":[{"name":"luci-app-airsim-0.1.0-r13.apk"}]},
	 {"tag_name":"v0.1.0-r20","draft":true,"assets":[{"name":"luci-app-airsim-0.1.0-r20.apk"}]},
	 {"tag_name":"v9.0.0-r1","assets":[]},
	 {"tag_name":"<script>alert(1)</script>","assets":[]}
	]`), &releases); e != nil {
		t.Fatal(e)
	}
	for _, item := range []struct {
		v        string
		previews bool
		want     string
		tag      string
	}{
		{"0.1.0-r12", true, "available", "v0.1.0-r13"},
		{"0.1.0-r12", false, "ahead", "v0.1.0-r9"},
		{"0.1.0-r13", true, "current", "v0.1.0-r13"},
		{"0.1.0-dev", true, "unknown_version", "v0.1.0-r13"},
	} {
		r := selectRelease(releases, item.v, item.previews)
		if r["status"] != item.want || r["latest"] != item.tag {
			t.Fatal(r)
		}
	}
	if selectRelease(nil, "0.1.0-r13", true)["status"] != "no_release" {
		t.Fatal("empty channel")
	}
	if !newer("v0.1.0-r13", "v0.1.0-r9") || newer("0.1.0-r9", "0.1.0-r13") {
		t.Fatal("lexical version comparison")
	}
}
