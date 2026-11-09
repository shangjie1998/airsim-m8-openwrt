package main

// Release checks are independent of the phone service. Installation is explicit,
// serialized, signed, and only allowed with the phone service stopped.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const updateKeySHA = "0ce703e76ea4b702194c54b90534d7127874bb38ecda17f7ed6b1add2d2cf1de"
const updateInterval = 6 * time.Hour

type updatePolicy struct {
	Automatic bool `json:"automatic"`
	Previews  bool `json:"previews"`
}
type updateCache struct {
	CheckedAt time.Time      `json:"checked_at"`
	Previews  bool           `json:"previews"`
	Result    map[string]any `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}
type updateJob struct {
	Phase     string    `json:"phase"`
	Target    string    `json:"target,omitempty"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updated_at"`
}

func updateRead(data, name string, value any) error {
	b, e := os.ReadFile(filepath.Join(data, name))
	if e != nil {
		return e
	}
	return json.Unmarshal(b, value)
}
func updateWrite(data, name string, value any) error {
	if e := os.MkdirAll(data, 0700); e != nil {
		return e
	}
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(data, ".update-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), filepath.Join(data, name))
}
func updatePolicyRead(data string) (updatePolicy, error) {
	p := updatePolicy{Automatic: true, Previews: true}
	e := updateRead(data, "update-policy.json", &p)
	if errors.Is(e, os.ErrNotExist) {
		e = nil
	}
	return p, e
}
func updateFileLock(data, name string) (*os.File, error) {
	if e := os.MkdirAll(data, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(data, name), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("另一项更新操作正在进行，请稍后再试")
	}
	return f, nil
}
func checkUpdatesCached(ctx context.Context, data string, previews, force bool) (map[string]any, error) {
	lock, e := updateFileLock(data, "update-check.lock")
	if e != nil {
		return nil, e
	}
	defer lock.Close()
	var cache updateCache
	if !force && updateRead(data, "update-cache.json", &cache) == nil && cache.Previews == previews && time.Since(cache.CheckedAt) >= 0 && time.Since(cache.CheckedAt) < updateInterval {
		return cache.Result, nil
	}
	r, e := checkUpdates(ctx, previews)
	cache = updateCache{CheckedAt: time.Now().UTC(), Previews: previews, Result: r}
	if e != nil {
		cache.Error = e.Error()
	}
	if err := updateWrite(data, "update-cache.json", cache); err != nil {
		return nil, errors.New("无法保存更新检查结果")
	}
	return r, e
}
func updateMonitor(ctx context.Context, data string) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		p, e := updatePolicyRead(data)
		if e == nil && p.Automatic {
			_, _ = checkUpdatesCached(ctx, data, p.Previews, false)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func updateStatus(data string) map[string]any {
	p, e := updatePolicyRead(data)
	if e != nil {
		return map[string]any{"error": "无法读取更新偏好"}
	}
	var cache updateCache
	_ = updateRead(data, "update-cache.json", &cache)
	if cache.Result != nil {
		cache.Result["current"] = version
		if latest, ok := cache.Result["latest"].(string); ok {
			switch {
			case newer(latest, version):
				cache.Result["status"] = "available"
			case newer(version, latest):
				cache.Result["status"] = "ahead"
			default:
				cache.Result["status"] = "current"
			}
		}
	}
	j := updateJob{Phase: "idle", Message: "尚未执行升级"}
	_ = updateRead(data, "update-job.json", &j)
	lock, err := updateFileLock(data, "update-install.lock")
	busy := err != nil
	if lock != nil {
		lock.Close()
	}
	if !busy && j.Phase != "idle" && j.Phase != "succeeded" && j.Phase != "failed" {
		j.Phase = "interrupted"
		j.Message = "上次更新中断，请检查已安装版本；不会自动重试"
	}
	return map[string]any{"ok": true, "policy": p, "cache": cache, "job": j, "busy": busy, "current": version}
}
func runUpdateCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	return c.Output()
}

// Pure preflight parser, also used with fixture outputs in tests. Do not infer
// idle from absent/failed status or from the voice-only CLCC projection.
func validateUpdateIdle(clcc string, sms map[string]any) error {
	lines := strings.Split(strings.TrimSpace(clcc), "\n")
	if strings.TrimSpace(lines[len(lines)-1]) != "OK" || strings.Contains(clcc, "+CLCC:") || strings.Contains(clcc, "ERROR") {
		return errors.New("基带有通话或状态未知，请结束通话后再更新")
	}
	flag := func(v any) bool { return v == true || v == float64(1) }
	if flag(sms["running"]) || !(flag(sms["none"]) || flag(sms["done"])) {
		return errors.New("短信任务进行中或状态未知，请稍后更新")
	}
	return nil
}
func updatePreflight(ctx context.Context) error {
	if err := nativeSMSUpdateIdle(ctx, runUpdateCommand); err != nil {
		return err
	}
	if legacyServiceListening() {
		return errors.New("请先在设备概览关闭 AirSIM 服务，再点击更新")
	}
	b, e := runUpdateCommand(ctx, "ubus", "call", "service", "list", `{"name":"airsim"}`)
	if e != nil {
		return errors.New("无法确认电话服务已停止")
	}
	var services map[string]any
	if json.Unmarshal(b, &services) != nil {
		return errors.New("服务状态无效")
	}
	for _, v := range object(object(services["airsim"])["instances"]) {
		if flagValue(object(v)["running"]) {
			return errors.New("请先关闭 AirSIM 服务")
		}
	}
	b, e = runUpdateCommand(ctx, "mu300-at", "-t", "3", "AT+CLCC")
	if e != nil {
		return errors.New("无法确认基带通话状态")
	}
	clcc := string(b)
	b, e = runUpdateCommand(ctx, "ubus", "call", "mu300dash", "sms_job")
	if e != nil {
		return errors.New("无法确认短信任务状态")
	}
	var sms map[string]any
	if json.Unmarshal(b, &sms) != nil {
		return errors.New("短信状态无效")
	}
	if e = validateUpdateIdle(clcc, sms); e != nil {
		return e
	}
	return validateUpdatePlatform(ctx, runUpdateCommand, exec.LookPath)
}

func nativeSMSUpdateIdle(ctx context.Context, run func(context.Context, string, ...string) ([]byte, error)) error {
	output, err := run(ctx, "pidof", "mu300-sms")
	var exitErr interface{ ExitCode() int }
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(strings.TrimSpace(string(output))) == 0 {
		return nil
	}
	return errors.New("原生短信操作仍在进行或状态未知，请稍后再更新")
}

// Installation requires executable capabilities, not a firmware/kernel allowlist.
// Audio driver ABI compatibility remains the installed audio stack's responsibility.
func validateUpdatePlatform(ctx context.Context, run func(context.Context, string, ...string) ([]byte, error), lookup func(string) (string, error)) error {
	arch, e := run(ctx, "uname", "-m")
	if e != nil || strings.TrimSpace(string(arch)) != "aarch64" {
		return errors.New("此安装包需要 aarch64 架构")
	}
	for _, tool := range []string{"apk", "mu300-at", "mu300-voice", "mu300-audio-dsp"} {
		if _, e := lookup(tool); e != nil {
			return fmt.Errorf("缺少 %s，请先安装设备所需依赖", tool)
		}
	}
	return nil
}

func allowedReleaseDownload(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func downloadUpdate(ctx context.Context, raw, path string, limit int64) error {
	if !allowedReleaseDownload(raw) {
		return errors.New("更新下载地址无效")
	}
	client := &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || !allowedReleaseDownload(r.URL.String()) {
			return errors.New("更新下载跳转被拒绝")
		}
		return nil
	}}
	req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
	resp, e := client.Do(req)
	if e != nil {
		return errors.New("下载失败，请检查网络后重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("Release 文件不可下载或尚未发布完整")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	n, e := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	if e != nil || n > limit {
		return errors.New("下载不完整或文件超出限制")
	}
	return f.Sync()
}
func checksumFor(manifest []byte, name string) (string, error) {
	found := ""
	for _, line := range strings.Split(string(manifest), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		if found != "" {
			return "", errors.New("校验清单存在重复文件")
		}
		decoded, e := hex.DecodeString(f[0])
		if e != nil || len(decoded) != 32 {
			return "", errors.New("校验和无效")
		}
		found = strings.ToLower(f[0])
	}
	if found == "" {
		return "", errors.New("Release 缺少安装包校验和")
	}
	return found, nil
}
func fileSHA(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func validatePackageMetadata(text, tag string) error {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "paths:") {
			break
		}
		for _, key := range []string{"name", "version", "arch"} {
			if strings.HasPrefix(line, "  "+key+": ") {
				if values[key] != "" {
					return errors.New("安装包元数据重复")
				}
				values[key] = strings.TrimPrefix(line, "  "+key+": ")
			}
		}
	}
	if values["name"] != "luci-app-airsim" || values["version"] != strings.TrimPrefix(tag, "v") || values["arch"] != "aarch64_generic" {
		return errors.New("安装包名称、版本或架构与目标不匹配")
	}
	return nil
}

func applyUpdatePackage(ctx context.Context, run func(context.Context, string, ...string) ([]byte, error), preflight func() error, report func(string, string), keys, pkg, tag string) error {
	report("verifying", "正在验证包签名、版本和设备状态")
	if _, e := run(ctx, "apk", "--keys-dir", keys, "verify", pkg); e != nil {
		return errors.New("APK 签名验证失败，未安装")
	}
	meta, e := run(ctx, "apk", "adbdump", pkg)
	if e != nil {
		return errors.New("无法读取已签名安装包信息")
	}
	if e = validatePackageMetadata(string(meta), tag); e != nil {
		return e
	}
	if e = preflight(); e != nil {
		return e
	}
	report("installing", "正在安装已验证的新版本，请勿断电或关闭设备")
	if _, e = run(ctx, "apk", "--keys-dir", keys, "--no-network", "add", "--upgrade", pkg); e != nil {
		return errors.New("包管理器升级失败；请检查插件状态，未自动重试")
	}
	installed, e := run(ctx, "/usr/lib/airsim/airsim-mu300d", "--manage", "get")
	var v struct {
		Version string `json:"version"`
	}
	if e != nil || json.Unmarshal(installed, &v) != nil || v.Version != strings.TrimPrefix(tag, "v") {
		return errors.New("安装后的版本未确认，请检查插件状态")
	}
	return nil
}

func beginUpdate(data, tag string) (map[string]any, error) {
	if _, ok := versionParts(tag); !ok || !newer(tag, version) {
		return nil, errors.New("仅允许安装明确的新版本，不能重复安装或降级")
	}
	lock, e := updateFileLock(data, "update-install.lock")
	if e != nil {
		return nil, e
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if e = updatePreflight(ctx); e != nil {
		return nil, e
	}
	// Copy the running updater so apk can replace its original executable safely.
	work, e := os.MkdirTemp("/tmp", "airsim-upgrade-")
	if e != nil {
		return nil, e
	}
	exe, e := os.Executable()
	if e != nil {
		os.RemoveAll(work)
		return nil, e
	}
	src, e := os.Open(exe)
	if e != nil {
		os.RemoveAll(work)
		return nil, e
	}
	defer src.Close()
	worker := filepath.Join(work, "worker")
	dst, e := os.OpenFile(worker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if e != nil {
		os.RemoveAll(work)
		return nil, e
	}
	_, e = io.Copy(dst, src)
	closeErr := dst.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		os.RemoveAll(work)
		return nil, e
	}
	j := updateJob{Phase: "queued", Target: tag, Message: "升级已提交，正在校验 Release", UpdatedAt: time.Now().UTC()}
	if e = updateWrite(data, "update-job.json", j); e != nil {
		os.RemoveAll(work)
		return nil, e
	}
	cmd := exec.Command(worker, "--update-worker", tag, "--data", data)
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e = cmd.Start(); e != nil {
		os.RemoveAll(work)
		j.Phase = "failed"
		j.Message = "无法启动更新任务"
		_ = updateWrite(data, "update-job.json", j)
		return nil, e
	}
	_ = cmd.Process.Release()
	return map[string]any{"ok": true, "job": j}, nil
}

func installUpdate(data, tag string) error {
	// FD 3 is the parent's exclusive install lock, retained until process exit.
	lock := os.NewFile(3, "update-install.lock")
	if lock == nil {
		return errors.New("更新锁缺失")
	}
	defer lock.Close()
	if e := syscall.Flock(3, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return e
	}
	exe, _ := os.Executable()
	work := filepath.Dir(exe)
	if !strings.HasPrefix(work, "/tmp/airsim-upgrade-") {
		return errors.New("更新工作目录无效")
	}
	defer os.RemoveAll(work)
	set := func(phase, message string) {
		_ = updateWrite(data, "update-job.json", updateJob{Phase: phase, Target: tag, Message: message, UpdatedAt: time.Now().UTC()})
	}
	e := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if !newer(tag, version) {
			return errors.New("目标版本不是更新版本")
		}
		p, e := updatePolicyRead(data)
		if e != nil {
			return e
		}
		r, e := checkUpdates(ctx, p.Previews)
		if e != nil {
			return e
		}
		if r["latest"] != tag || r["status"] != "available" {
			return errors.New("Release 或更新频道已变化，请重新检查更新")
		}
		set("downloading", "正在下载安装包；电话服务保持关闭")
		name := "luci-app-airsim-" + strings.TrimPrefix(tag, "v") + ".apk"
		base := releasePage + "/download/v" + strings.TrimPrefix(tag, "v") + "/"
		for _, f := range []struct {
			name  string
			limit int64
		}{{"SHA256SUMS", 65536}, {"airsim-dev.pem", 16384}, {name, 64 << 20}} {
			if e = downloadUpdate(ctx, base+f.name, filepath.Join(work, f.name), f.limit); e != nil {
				return e
			}
		}
		keyHash, e := fileSHA(filepath.Join(work, "airsim-dev.pem"))
		if e != nil || keyHash != updateKeySHA {
			return errors.New("签名公钥与内置指纹不符，拒绝安装")
		}
		manifest, e := os.ReadFile(filepath.Join(work, "SHA256SUMS"))
		if e != nil {
			return e
		}
		want, e := checksumFor(manifest, name)
		if e != nil {
			return e
		}
		got, e := fileSHA(filepath.Join(work, name))
		if e != nil || want != got {
			return errors.New("安装包 SHA-256 校验失败")
		}
		keys := filepath.Join(work, "keys")
		if e = os.Mkdir(keys, 0700); e != nil {
			return e
		}
		key, e := os.ReadFile(filepath.Join(work, "airsim-dev.pem"))
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(keys, "airsim-dev.pem"), key, 0600); e != nil {
			return e
		}
		pkg := filepath.Join(work, name)
		guard, e := managementLock(data, true)
		if e != nil {
			return e
		}
		defer guard.Close()
		return applyUpdatePackage(ctx, runUpdateCommand, func() error { return updatePreflight(ctx) }, set, keys, pkg, tag)
	}()
	if e != nil {
		set("failed", e.Error())
		return e
	}
	set("succeeded", "更新成功。请刷新页面，再到设备概览启动服务。")
	return nil
}

func updateManagement(action, data string) any {
	var result map[string]any
	var err error
	switch action {
	case "updates-status":
		return updateStatus(data)
	case "updates-policy":
		var p updatePolicy
		d := json.NewDecoder(io.LimitReader(os.Stdin, 1024))
		d.DisallowUnknownFields()
		if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
			err = errors.New("更新偏好格式无效")
		} else {
			err = updateWrite(data, "update-policy.json", p)
		}
		if err == nil {
			return updateStatus(data)
		}
	case "update-install":
		var input struct {
			Tag string `json:"tag"`
		}
		d := json.NewDecoder(io.LimitReader(os.Stdin, 1024))
		d.DisallowUnknownFields()
		if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF {
			err = errors.New("升级请求格式无效")
		} else {
			result, err = beginUpdate(data, input.Tag)
		}
	}
	if err != nil {
		return map[string]any{"error": fmt.Sprint(err)}
	}
	return result
}
