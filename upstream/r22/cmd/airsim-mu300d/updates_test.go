package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type smsProcessExit int

func (e smsProcessExit) Error() string { return "fixture process status" }
func (e smsProcessExit) ExitCode() int { return int(e) }

func TestUpdateBlocksNativeSMSAndUnknownProcessState(t *testing.T) {
	for _, tc := range []struct {
		output string
		err    error
		idle   bool
	}{
		{"", smsProcessExit(1), true},
		{"123", nil, false},
		{"", nil, false},
		{"", smsProcessExit(127), false},
		{"", context.DeadlineExceeded, false},
		{"123", smsProcessExit(1), false},
	} {
		err := nativeSMSUpdateIdle(context.Background(), func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "pidof" || len(args) != 1 || args[0] != "mu300-sms" {
				t.Fatal("wrong native SMS check")
			}
			return []byte(tc.output), tc.err
		})
		if (err == nil) != tc.idle {
			t.Fatal("native SMS idle incorrectly inferred")
		}
	}
}

func TestUpdateInstallTransactionFailsClosed(t *testing.T) {
	for _, mode := range []string{"success", "bad-signature", "wrong-version", "busy", "apk-failure", "installed-version-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			applied, verified, preflight := 0, false, false
			run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				a := strings.Join(args, " ")
				switch {
				case strings.Contains(a, " verify "):
					if mode == "bad-signature" {
						return nil, errors.New("bad signature")
					}
					verified = true
					return nil, nil
				case strings.HasPrefix(a, "adbdump "):
					if !verified {
						t.Fatal("read unverified metadata")
					}
					v := "0.1.0-r15"
					if mode == "wrong-version" {
						v = "0.1.0-r12"
					}
					return []byte("info:\n  name: luci-app-airsim\n  version: " + v + "\n  arch: aarch64_generic\npaths:"), nil
				case strings.Contains(a, " add "):
					if !verified || !preflight || !strings.Contains(a, "--no-network") || strings.Contains(a, "allow-untrusted") {
						t.Fatal("unsafe install sequence")
					}
					applied++
					if mode == "apk-failure" {
						return nil, errors.New("apk failed")
					}
					return nil, nil
				default:
					if applied != 1 {
						t.Fatal("queried installed version before apply")
					}
					if mode == "installed-version-mismatch" {
						return []byte(`{"version":"0.1.0-r12"}`), nil
					}
					return []byte(`{"version":"0.1.0-r15"}`), nil
				}
			}
			e := applyUpdatePackage(context.Background(), run, func() error {
				preflight = true
				if mode == "busy" {
					return errors.New("busy")
				}
				return nil
			}, func(string, string) {}, "/keys", "/pkg.apk", "v0.1.0-r15")
			if (e == nil) != (mode == "success") {
				t.Fatal(mode, e)
			}
			if (mode == "bad-signature" || mode == "wrong-version" || mode == "busy") && applied != 0 {
				t.Fatal("unsafe package installed")
			}
			if applied > 1 {
				t.Fatal("install retried")
			}
		})
	}
}

func TestUpdateIdleRequiresPositiveEvidence(t *testing.T) {
	for _, tc := range []struct {
		clcc string
		sms  map[string]any
		ok   bool
	}{
		{"\r\nOK\r\n", map[string]any{"none": float64(1)}, true},
		{"OK", map[string]any{"done": float64(1), "running": float64(0)}, true},
		{"NOTOK", map[string]any{"none": float64(1)}, false},
		{"ERROR\nOK", map[string]any{"none": float64(1)}, false},
		{"", map[string]any{"none": float64(1)}, false},
		{"+CLCC: 1,0,3,3,0\nOK", map[string]any{"none": float64(1)}, false},
		{"OK", nil, false},
		{"OK", map[string]any{"done": true, "running": true}, false},
	} {
		if e := validateUpdateIdle(tc.clcc, tc.sms); (e == nil) != tc.ok {
			t.Fatalf("idle decision incorrect: %q %v %v", tc.clcc, tc.sms, e)
		}
	}
}
func TestUpdateDownloadBoundaries(t *testing.T) {
	for _, s := range []string{"https://github.com/owner/repo/file", "https://release-assets.githubusercontent.com/file?signature=x", "https://objects.githubusercontent.com/a"} {
		if !allowedReleaseDownload(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"http://github.com/a", "https://github.com.evil.test/a", "https://user@github.com/a", "https://github.com:444/a", "file:///tmp/pkg", "https://127.0.0.1/a"} {
		if allowedReleaseDownload(s) {
			t.Fatal("unsafe redirect", s)
		}
	}
	dir := t.TempDir()
	if downloadUpdate(context.Background(), "file:///tmp/pkg", filepath.Join(dir, "pkg"), 10) == nil {
		t.Fatal("download accepted file URL")
	}
	if _, e := os.Stat(filepath.Join(dir, "pkg")); !os.IsNotExist(e) {
		t.Fatal("bad URL wrote a file")
	}
}
func TestUpdateChecksumAndSignedIdentity(t *testing.T) {
	h := strings.Repeat("a", 64)
	name := "luci-app-airsim-0.1.0-r15.apk"
	if got, e := checksumFor([]byte(h+"  "+name+"\n"), name); e != nil || got != h {
		t.Fatal(got, e)
	}
	for _, s := range []string{h + "  other.apk", "bad  " + name, h + "  " + name + "\n" + h + "  " + name} {
		if _, e := checksumFor([]byte(s), name); e == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	meta := "info:\n  name: luci-app-airsim\n  version: 0.1.0-r15\n  arch: aarch64_generic\npaths:\n  name: ignored"
	if e := validatePackageMetadata(meta, "v0.1.0-r15"); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{strings.Replace(meta, "0.1.0-r15", "0.1.0-r12", 1), strings.Replace(meta, "aarch64_generic", "x86_64", 1), strings.Replace(meta, "luci-app-airsim", "unrelated", 1), "  name: luci-app-airsim\n" + meta} {
		if validatePackageMetadata(s, "v0.1.0-r15") == nil {
			t.Fatal("wrong signed identity accepted")
		}
	}
}
func TestUpdatePersistenceInterruptedJobAndCache(t *testing.T) {
	dir := t.TempDir()
	p, e := updatePolicyRead(dir)
	if e != nil || !p.Automatic || !p.Previews {
		t.Fatal("default check policy", e)
	}
	if e = updateWrite(dir, "update-policy.json", updatePolicy{}); e != nil {
		t.Fatal(e)
	}
	p, e = updatePolicyRead(dir)
	if e != nil || p.Automatic || p.Previews {
		t.Fatal("disable not persisted")
	}
	info, _ := os.Stat(filepath.Join(dir, "update-policy.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("unsafe mode")
	}
	_ = updateWrite(dir, "update-job.json", updateJob{Phase: "installing", Target: "v0.1.0-r15"})
	lock, e := updateFileLock(dir, "update-install.lock")
	if e != nil {
		t.Fatal(e)
	}
	if !updateStatus(dir)["busy"].(bool) {
		t.Fatal("lost running job")
	}
	lock.Close()
	if updateStatus(dir)["job"].(updateJob).Phase != "interrupted" {
		t.Fatal("stale job shown as still installing")
	}
	_ = updateWrite(dir, "update-cache.json", updateCache{CheckedAt: time.Now(), Previews: true, Result: map[string]any{"status": "current", "latest": "v0.1.0-r15"}})
	r, e := checkUpdatesCached(context.Background(), dir, true, false)
	if e != nil || r["latest"] != "v0.1.0-r15" {
		t.Fatal("fresh cache not used", e)
	}
}
func TestUpdateRejectsUnsafeTagWithoutSpawning(t *testing.T) {
	for _, tag := range []string{"../../tmp/bad", "v0.0.0-r0", "v1.0.0-r1;reboot", "latest", ""} {
		if _, e := beginUpdate(t.TempDir(), tag); e == nil {
			t.Fatal("unsafe tag accepted", tag)
		}
	}
}

func TestUpdatePlatformRequiresCapabilitiesNotReleaseNumbers(t *testing.T) {
	for _, tc := range []struct {
		name, arch, missing string
		queryError          bool
		ok                  bool
	}{
		{name: "aarch64-without-version-files", arch: "aarch64\n", ok: true},
		{name: "wrong-architecture", arch: "x86_64"},
		{name: "unknown-architecture", queryError: true},
		{name: "missing-audio", arch: "aarch64", missing: "mu300-voice"},
		{name: "missing-dsp", arch: "aarch64", missing: "mu300-audio-dsp"},
		{name: "missing-apk", arch: "aarch64", missing: "apk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "uname" || strings.Join(args, " ") != "-m" {
					t.Fatalf("unexpected version query or device mutation: %s %v", name, args)
				}
				if tc.queryError {
					return nil, errors.New("unavailable")
				}
				return []byte(tc.arch), nil
			}
			lookup := func(name string) (string, error) {
				if name == tc.missing {
					return "", os.ErrNotExist
				}
				return "/usr/bin/" + name, nil
			}
			if e := validateUpdatePlatform(context.Background(), run, lookup); (e == nil) != tc.ok {
				t.Fatalf("platform result = %v, want allowed=%v", e, tc.ok)
			}
		})
	}
}
