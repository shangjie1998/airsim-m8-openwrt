package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"airsim.local/mu300/internal/audio"
	"airsim.local/mu300/internal/gateway"
	"airsim.local/mu300/internal/pairing"
	"github.com/grandcat/zeroconf"
)

var version = "0.1.0-dev"

type daemon struct {
	adMu          sync.Mutex
	advertisement *zeroconf.Server
	ifname        string
	mu            sync.Mutex
	session       *pairing.Session
	bridge        audio.Bridge
	lan           string
	deviceID      string
	client        *http.Client
	paired        bool
	callMu        sync.Mutex
	gateway       *gateway.Gateway
	settings      managementSettings
	ctx           context.Context
	netMu         sync.Mutex
	pairListener  net.Listener
	ctrlListener  net.Listener
	pcmListener   net.Listener
	pairServer    *http.Server
	ctrlServer    *http.Server
}

func main() {
	iface := flag.String("interface", "", "legacy trusted LAN interface override")
	data := flag.String("data", "/mnt/mu300-disk/extra/airsim/data", "persistent private state")
	manage := flag.String("manage", "", "rpcd management command")
	monitorUpdates := flag.Bool("update-monitor", false, "periodically check published plugin versions")
	updateWorker := flag.String("update-worker", "", "internal detached upgrade worker")
	runAgent := flag.Bool("run-agent", false, "supervise Agent with MU300 management policy")
	selfTest := flag.Duration("audio-self-test", 0, "explicit no-call DSP self-test, 1s..30s")
	trial := flag.Bool("early-media-trial", false, "explicit 10-minute lab window; separately arm one current call for 60s")
	trialArm := flag.Bool("early-media-trial-arm", false, "locally authorize only the current owned outgoing call for receive-only trial")
	trialRevoke := flag.Bool("early-media-trial-revoke", false, "remove the local early-media trial permission")
	flag.Parse()
	if *trialArm || *trialRevoke {
		var err error
		if *trialArm && *trialRevoke {
			log.Fatal("choose arm or revoke")
		}
		if *trialArm {
			err = armEarlyMediaTrial(*data)
		} else {
			err = os.Remove(filepath.Join(*data, "early-media-trial.json"))
			if os.IsNotExist(err) {
				err = nil
			}
		}
		if err != nil {
			log.Fatal("early-media trial command failed: ", err)
		}
		log.Print("early-media trial permission updated; no call control or recording performed")
		return
	}
	if *updateWorker != "" {
		if installUpdate(*data, *updateWorker) != nil {
			os.Exit(1)
		}
		return
	}
	if *monitorUpdates {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer cancel()
		updateMonitor(ctx, *data)
		return
	}
	if *manage != "" {
		json.NewEncoder(os.Stdout).Encode(managementCommand(*manage, *data))
		return
	}
	if *runAgent {
		if e := startManagedAgent(*data); e != nil {
			log.Fatal("managed Agent stopped")
		}
		return
	}
	log.SetFlags(log.LstdFlags | log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if *selfTest != 0 {
		b := &audio.Bridge{}
		if e := b.SelfTest(ctx, *selfTest); e != nil {
			log.Fatal(e)
		}
		json.NewEncoder(os.Stdout).Encode(b.Status())
		return
	}
	lock, e := managementLock(*data, false)
	if e != nil {
		log.Fatal(e)
	}
	defer lock.Close()
	settings, e := loadSettings(*data)
	if e != nil {
		log.Fatal(e)
	}
	if *iface != "" {
		settings.Interface = *iface
	}
	if e := settings.validate(); e != nil {
		log.Fatal(e)
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		log.Fatal(err)
	}
	id, e := identity(filepath.Join(*data, "agent-id"))
	if e != nil {
		log.Fatal(e)
	}
	lan, e := lanAddress(settings.Interface)
	if e != nil {
		log.Fatal(e)
	}
	d := &daemon{lan: lan, ifname: settings.Interface, settings: settings, deviceID: id, client: &http.Client{Timeout: 10 * time.Second}}
	d.gateway, e = gateway.New(filepath.Join(*data, "vowlan-clients.json"), id)
	if e != nil {
		log.Fatal(e)
	}
	// A dropped audio stream is the earliest sign of a remote hang-up: ask the agent
	// for an immediate call-state read so the UI does not wait for the next poll.
	audio.OnStreamFault = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:8575/api/calls/refresh", nil)
		if resp, e := d.client.Do(req); e == nil {
			resp.Body.Close()
		}
	}
	go keepLogsBounded(ctx)
	d.gateway.SetEarlyMediaBackend(gateway.NewMU300EarlyMediaBackend(d.bridge.OpenEarlyMedia))
	if *trial {
		d.gateway.SetEarlyMediaBackend(gateway.NewMU300EarlyMediaTrialBackend(d.bridge.OpenEarlyMedia, filepath.Join(*data, "early-media-trial.json")))
	}
	lanInterface, e := net.InterfaceByName(settings.Interface)
	if e != nil {
		log.Fatal(e)
	}
	d.ctx = ctx
	gwAd, e := zeroconf.Register("AirSIM-VIPME-M8-"+id[len(id)-8:], "_airsim-vowlan._tcp", "local.", settings.ControlPort,
		[]string{"v=1", "agent_id=" + id, "agent_kind=mu300", "device_name=AirSIM VIPME-M8", "manufacturer=ZTE", "model=VIPME-M8", "host=" + lan, "control_port=" + portText(settings.ControlPort), "pcm_port=" + portText(settings.PCMPort), "caps=control,pcm"}, []net.Interface{*lanInterface})
	if e != nil {
		log.Fatal(e)
	}
	defer gwAd.Shutdown()
	d.advertisement = gwAd
	d.pcmListener, e = net.Listen("tcp", net.JoinHostPort(lan, portText(settings.PCMPort)))
	if e != nil {
		log.Fatal(e)
	}
	defer d.pcmListener.Close()
	go d.servePCM(ctx, d.pcmListener)
	listener, e := net.Listen("tcp", "127.0.0.1:7580")
	if e != nil {
		log.Fatal(e)
	}
	defer listener.Close()
	var sessions sync.WaitGroup
	acceptedDone := make(chan struct{})
	go func() { <-ctx.Done(); listener.Close() }()
	go func() {
		defer close(acceptedDone)
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			sessions.Add(1)
			go func() { defer sessions.Done(); d.bridge.Handle(ctx, c) }()
		}
	}()
	admin := http.NewServeMux()
	admin.HandleFunc("/status", d.status)
	// Internal Agent transport only; never registered on the LAN control mux.
	admin.HandleFunc("/internal/cloud-early-media", d.gateway.ServeCloudEarlyMedia)
	admin.HandleFunc("/pair/start", d.startPair)
	admin.HandleFunc("/pair/cancel", d.cancelPair)
	admin.HandleFunc("/pair/clients", d.pairClients)
	admin.HandleFunc("/pair/remove", d.removePairClient)
	admin.HandleFunc("/number/refresh", d.refreshNumber)
	admin.HandleFunc("/call/action", d.callAction)
	// 运行期重绑：切到外部 Wi-Fi 上行后，把 pair/control/PCM 监听与 Bonjour 广告
	// 搬到新地址。只绑 127.0.0.1，外部不可达；由 rpcd airsim-net 内部调用。
	admin.HandleFunc("/rebind", d.rebind)
	public := http.NewServeMux()
	public.HandleFunc("/pair/v1/info", d.pairInfo)
	public.HandleFunc("/pair/v1/claim", d.claim)
	adminServer := &http.Server{Addr: "127.0.0.1:7581", Handler: admin, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 45 * time.Second, MaxHeaderBytes: 8192}
	d.pairServer = &http.Server{Addr: net.JoinHostPort(lan, portText(settings.PairPort)), Handler: public, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	d.ctrlServer = &http.Server{Addr: net.JoinHostPort(lan, portText(settings.ControlPort)), Handler: d.gateway, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 80 * time.Second, MaxHeaderBytes: 8192}
	d.pairListener, e = net.Listen("tcp", d.pairServer.Addr)
	if e != nil {
		log.Fatal(e)
	}
	d.ctrlListener, e = net.Listen("tcp", d.ctrlServer.Addr)
	if e != nil {
		log.Fatal(e)
	}
	go func() {
		if e := adminServer.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Printf("admin listener failed: %s", adminServer.Addr)
			cancel()
		}
	}()
	go func() {
		if e := d.pairServer.Serve(d.pairListener); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Printf("pair listener failed: %s", d.pairServer.Addr)
			cancel()
		}
	}()
	go func() {
		if e := d.ctrlServer.Serve(d.ctrlListener); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Printf("control listener failed: %s", d.ctrlServer.Addr)
			cancel()
		}
	}()
	log.Printf("VIPME-M8 plugin ready; pairing port %d", settings.PairPort)
	<-ctx.Done()
	d.closePair("")
	adminServer.Close()
	d.pairServer.Close()
	d.ctrlServer.Close()
	listener.Close()
	d.pcmListener.Close()
	d.gateway.Shutdown()
	<-acceptedDone
	sessions.Wait()
}
func identity(path string) (string, error) {
	if b, e := os.ReadFile(path); e == nil && len(b) > 0 {
		return string(b), nil
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	id := "mu300-" + base64.RawURLEncoding.EncodeToString(b)
	return id, os.WriteFile(path, []byte(id), 0600)
}
func lanAddress(name string) (string, error) {
	i, e := net.InterfaceByName(name)
	if e != nil {
		return "", e
	}
	addrs, e := i.Addrs()
	if e != nil {
		return "", e
	}
	for _, a := range addrs {
		ip, _, _ := net.ParseCIDR(a.String())
		if ip.To4() != nil && ip.IsPrivate() {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no private IPv4 on %s", name)
}

// servePCM 接 PCM 直连；监听被替换时旧 Accept 循环随旧 listener 退出。
func (d *daemon) servePCM(ctx context.Context, l net.Listener) {
	go func() { <-ctx.Done(); l.Close() }()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go d.gateway.ServePCM(ctx, c)
	}
}

func privateIPv4(ifc *net.Interface) (string, error) {
	addrs, e := ifc.Addrs()
	if e != nil {
		return "", e
	}
	for _, a := range addrs {
		ip, _, _ := net.ParseCIDR(a.String())
		if ip.To4() != nil && ip.IsPrivate() {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no private IPv4 on %s", ifc.Name)
}

func interfaceForIP(ip net.IP) (*net.Interface, error) {
	ifcs, e := net.Interfaces()
	if e != nil {
		return nil, e
	}
	for i := range ifcs {
		addrs, e := ifcs[i].Addrs()
		if e != nil {
			continue
		}
		for _, a := range addrs {
			netip, _, _ := net.ParseCIDR(a.String())
			if netip.Equal(ip) {
				return &ifcs[i], nil
			}
		}
	}
	return nil, fmt.Errorf("no interface for %s", ip)
}

func defaultRouteInterface() (*net.Interface, error) {
	out, e := exec.Command("ip", "route").Output()
	if e != nil {
		return nil, e
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "default") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "dev" && i+1 < len(fields) {
				return net.InterfaceByName(fields[i+1])
			}
		}
	}
	return nil, errors.New("no default route")
}

// rebindTarget 决定当前该对外宣告哪个直连地址：优先 rpcd 写好的目标文件，
// 其次默认路由所在口，最后退回配置的 LAN 口。只读，不写配置。
func (d *daemon) rebindTarget() (string, *net.Interface, error) {
	if b, e := os.ReadFile("/tmp/airsim-direct-host"); e == nil {
		host := strings.TrimSpace(string(b))
		if ip := net.ParseIP(host); ip != nil {
			if ifc, e := interfaceForIP(ip); e == nil {
				return host, ifc, nil
			}
		}
	}
	if ifc, e := defaultRouteInterface(); e == nil {
		if host, e := privateIPv4(ifc); e == nil {
			return host, ifc, nil
		}
	}
	if ifc, e := net.InterfaceByName(d.ifname); e == nil {
		if host, e := privateIPv4(ifc); e == nil {
			return host, ifc, nil
		}
	}
	return "", nil, errors.New("no usable direct address")
}

// rebind 运行期把 pair/control/PCM 监听与 Bonjour 主广告搬到新地址。
// 先建新监听、成功再拆旧的；已建立的 TCP 连接不随 listener 关闭而断开。
func (d *daemon) rebind(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	host, ifc, e := d.rebindTarget()
	if e != nil {
		write(w, 500, map[string]any{"error": "no usable direct address"})
		return
	}
	d.netMu.Lock()
	defer d.netMu.Unlock()
	if host == d.lan {
		write(w, 200, map[string]any{"ok": true, "host": host, "changed": false})
		return
	}
	pairL, e1 := net.Listen("tcp", net.JoinHostPort(host, portText(d.settings.PairPort)))
	ctrlL, e2 := net.Listen("tcp", net.JoinHostPort(host, portText(d.settings.ControlPort)))
	pcmL, e3 := net.Listen("tcp", net.JoinHostPort(host, portText(d.settings.PCMPort)))
	if e1 != nil || e2 != nil || e3 != nil {
		pairL.Close()
		ctrlL.Close()
		pcmL.Close()
		write(w, 500, map[string]any{"error": "bind new listeners failed", "detail": fmt.Sprint(e1, e2, e3)})
		return
	}
	oldPair, oldCtrl, oldPcm := d.pairListener, d.ctrlListener, d.pcmListener
	d.pairListener, d.ctrlListener, d.pcmListener = pairL, ctrlL, pcmL
	d.lan = host
	d.ifname = ifc.Name
	go d.pairServer.Serve(pairL)
	go d.ctrlServer.Serve(ctrlL)
	go d.servePCM(d.ctx, pcmL)
	_ = oldPair.Close()
	_ = oldCtrl.Close()
	_ = oldPcm.Close()

	// 有进行中的配对会话时，advertisement 挂的是配对代理广告，不能顺手关掉；
	// 主服务广告留到下次空闲再重注册。
	d.mu.Lock()
	sessionActive := d.session != nil
	d.mu.Unlock()
	if !sessionActive {
		d.adMu.Lock()
		if d.advertisement != nil {
			d.advertisement.Shutdown()
			d.advertisement = nil
		}
		txt := []string{
			"v=1", "agent_id=" + d.deviceID, "agent_kind=mu300",
			"device_name=AirSIM VIPME-M8", "manufacturer=ZTE", "model=VIPME-M8",
			"host=" + host,
			"control_port=" + portText(d.settings.ControlPort),
			"pcm_port=" + portText(d.settings.PCMPort),
			"caps=control,pcm",
		}
		suffix := d.deviceID
		if len(suffix) > 8 {
			suffix = suffix[len(suffix)-8:]
		}
		if ad, err := zeroconf.Register("AirSIM-VIPME-M8-"+suffix, "_airsim-vowlan._tcp", "local.", d.settings.ControlPort, txt, []net.Interface{*ifc}); err == nil {
			d.advertisement = ad
		} else {
			log.Printf("rebind: bonjour re-register failed: %v", err)
		}
		d.adMu.Unlock()
	}
	log.Printf("rebind: direct listeners moved to %s (%s)", host, ifc.Name)
	write(w, 200, map[string]any{"ok": true, "host": host, "changed": true})
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func method(w http.ResponseWriter, r *http.Request, m string) bool {
	if r.Method != m {
		write(w, 405, map[string]string{"error": "method_not_allowed"})
		return false
	}
	return true
}
func (d *daemon) status(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	dev, e := audio.Discover()
	if e == nil {
		if dev.CallProbe == "mmcli" {
			e = audio.CheckMMTools()
		} else {
			e = audio.CheckTools()
		}
	}
	d.mu.Lock()
	var session any
	if d.session != nil && d.session.Expires > time.Now().UnixMilli() {
		session = map[string]any{"code": d.session.Code, "expires_at": d.session.Expires}
	}
	paired := d.paired
	d.mu.Unlock()
	calls := d.callsSnapshot(r.Context())
	write(w, 200, map[string]any{"product": "AirSIM for VIPME-M8", "version": version, "running": true, "lan_ip": d.lan, "pairing_port": d.settings.PairPort, "control_port": d.settings.ControlPort, "pcm_port": d.settings.PCMPort, "mode": d.settings.Mode, "bonjour_pairing": session != nil, "agent_id": d.deviceID, "pairing": session, "paired_this_boot": paired, "audio": d.bridge.Status(), "gateway": d.gateway.Diagnostics(), "devices": dev, "audio_available": e == nil, "agent": d.agentHealth(), "sim": d.agentGet("/api/status"), "relay": d.agentGet("/api/push/status"), "call_active": !calls.Available || len(calls.Calls) > 0, "calls": calls})
}

func (d *daemon) callActive() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8575/api/calls/status", nil)
	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var state struct {
		Active *json.RawMessage `json:"active"`
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&state) == nil && state.Active != nil
}

func (d *daemon) refreshNumber(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), "GET", "http://127.0.0.1:8575/api/sim/identity", nil)
	resp, err := d.client.Do(req)
	if err != nil {
		write(w, 502, map[string]string{"error": "无法连接 Agent，请确认服务已启动"})
		return
	}
	defer resp.Body.Close()
	var identity map[string]any
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&identity) != nil {
		write(w, 200, map[string]string{"error": "号码读取失败，请稍后重试"})
		return
	}
	write(w, 200, map[string]any{"ok": true, "phone_number": identity["phone_number"]})
}

// keepLogsBounded trims oversized log files. The device root is a single 4 GB
// partition and the native modem stack writes /var/log/e5-android-log.txt
// forever, so an unattended log must not be allowed to fill it.
func keepLogsBounded(ctx context.Context) {
	trim := func(path string, maxSize, keep int64) {
		info, e := os.Stat(path)
		if e != nil || info.Size() <= maxSize {
			return
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return
		}
		if int64(len(data)) > keep {
			data = data[int64(len(data))-keep:]
			if at := bytes.IndexByte(data, '\n'); at >= 0 {
				data = data[at+1:]
			}
		}
		_ = os.WriteFile(path, data, 0o644)
	}
	run := func() {
		trim("/var/log/e5-android-log.txt", 2*1024*1024, 256*1024)
		trim("/var/log/messages", 1024*1024, 256*1024)
	}
	run()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (d *daemon) agentGet(path string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8575"+path, nil)
	resp, e := d.client.Do(req)
	if e != nil {
		return map[string]any{}
	}
	defer resp.Body.Close()
	var data map[string]any
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&data) != nil {
		return map[string]any{}
	}
	// Only return selected status and this SIM's own number; never credentials.
	result := map[string]any{}
	for _, key := range []string{"sim_inserted", "signal_dbm", "reg_status_text", "phone_number", "phone_number_status", "phone_number_updated_at", "battery", "configured", "cloud_enabled", "command_connected", "management_blocked", "call_push_ready", "last_error", "relay_url"} {
		if value, ok := data[key]; ok {
			result[key] = value
		}
	}
	return result
}
func (d *daemon) agentHealth() any {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8575/api/health", nil)
	resp, e := d.client.Do(r)
	if e != nil {
		return map[string]any{"reachable": false}
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&v)
	return map[string]any{"reachable": resp.StatusCode == 200, "runtime_profile": v["runtime_profile"], "version": v["version"], "voice_backend": v["voice_backend"]}
}
func (d *daemon) startPair(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	s, e := pairing.New(time.Now())
	if e != nil {
		write(w, 500, map[string]string{"error": "pairing_creation_failed"})
		return
	}
	d.adMu.Lock()
	i, err := net.InterfaceByName(d.ifname)
	if err != nil {
		d.adMu.Unlock()
		write(w, 503, map[string]string{"error": "LAN unavailable"})
		return
	}
	ad, err := zeroconf.RegisterProxy("AirSIM-VIPME-M8-"+s.ID[:8], "_airsim-pair._tcp", "local.", d.settings.PairPort, "airsim-"+s.ID[:8], []string{d.lan}, []string{"v=1", "session=" + s.ID, "key=" + s.Public, fmt.Sprintf("expires=%d", s.Expires)}, []net.Interface{*i})
	if err != nil {
		d.adMu.Unlock()
		write(w, 503, map[string]string{"error": "pairing discovery unavailable"})
		return
	}
	ad.TTL(30)
	if d.advertisement != nil {
		d.advertisement.Shutdown()
	}
	d.advertisement = ad
	d.mu.Lock()
	d.session = s
	d.mu.Unlock()
	d.adMu.Unlock()
	time.AfterFunc(120*time.Second, func() { d.closePair(s.ID) })
	write(w, 200, map[string]any{"code": s.Code, "expires_at": s.Expires, "host": d.lan, "port": d.settings.PairPort})
}
func (d *daemon) cancelPair(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	d.closePair("")
	write(w, 200, map[string]bool{"ok": true})
}

func (d *daemon) closePair(id string) {
	d.adMu.Lock()
	defer d.adMu.Unlock()
	d.mu.Lock()
	if id != "" && (d.session == nil || d.session.ID != id) {
		d.mu.Unlock()
		return
	}
	d.session = nil
	d.mu.Unlock()
	if d.advertisement != nil {
		d.advertisement.Shutdown()
		d.advertisement = nil
	}
}
func (d *daemon) activeSession() *pairing.Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session == nil || d.session.Expires <= time.Now().UnixMilli() {
		return nil
	}
	return d.session
}
func (d *daemon) pairInfo(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	s := d.activeSession()
	if s == nil {
		write(w, 410, map[string]string{"error": "pairing_expired"})
		return
	}
	write(w, 200, s.Info())
}
func (d *daemon) claim(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	s := d.activeSession()
	if s == nil {
		write(w, 410, map[string]string{"error": "pairing_expired"})
		return
	}
	var claim pairing.Claim
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&claim) != nil {
		write(w, 400, map[string]string{"error": "invalid_claim"})
		return
	}
	if claim.WantDirect {
		write(w, 409, map[string]string{"error": "Manual-IP pairing is not available in this preview; use Bonjour automatic discovery"})
		return
	}
	plaintext, e := s.Open(claim, time.Now())
	if e != nil {
		write(w, 401, map[string]string{"error": "pairing_auth_failed"})
		return
	}
	defer clear(plaintext)
	// Open 已消费本次配对码：注册或持久化随后失败也要立即关掉窗口，
	// 不让用过的码被当成可重用；不重开、不重放注册。
	defer d.closePair(s.ID)
	var reg map[string]any
	if json.Unmarshal(plaintext, &reg) != nil {
		write(w, 400, map[string]string{"error": "invalid_registration"})
		return
	}
	localSecret, _ := reg["vowlan_secret"].(string)
	if d.gateway == nil {
		write(w, 500, map[string]string{"error": "gateway_key_storage_failed"})
		return
	}
	if d.gateway.CheckAdd(localSecret) != nil {
		write(w, 400, map[string]string{"error": "invalid_registration", "message": "配对密钥格式无效，请更正后重新获取配对码。"})
		return
	}
	prepareRegistration(reg, d.deviceID)
	body, _ := json.Marshal(reg)
	defer clear(body)
	req, _ := http.NewRequestWithContext(r.Context(), "POST", "http://127.0.0.1:8575/api/push/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, e := d.client.Do(req)
	if e != nil {
		write(w, 502, map[string]string{"error": "agent_unavailable"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		write(w, 502, map[string]string{"error": "agent_registration_rejected"})
		return
	}
	var result map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&result) != nil {
		write(w, 502, map[string]string{"error": "invalid_agent_response"})
		return
	}
	pushConfigured, ok := result["configured"].(bool)
	if !ok {
		write(w, 502, map[string]string{"error": "invalid_agent_response"})
		return
	}
	if d.gateway.Add(localSecret) != nil {
		write(w, 500, map[string]string{"error": "gateway_key_storage_failed"})
		return
	}
	// Agent HTTP 200 acknowledges validation and persistence. Its configured
	// field describes cloud push readiness, which can be false for a valid
	// pairing (disabled cloud, missing relay, or management policy). The phone
	// interprets configured on this endpoint as the pairing acknowledgement.
	// Keep readiness separate; never change the stored cloud preferences.
	result["push_configured"] = pushConfigured
	result["configured"] = true
	result["agent_id"] = d.deviceID
	d.mu.Lock()
	d.paired = true
	d.mu.Unlock()
	write(w, 200, result)
}

// These fields belong to the phone-side gateway, not the Go Agent's strict
// pushRegistration schema. MU300 has no Android notification-forwarding source.
// Preserve all cloud credentials; never log the decrypted registration.
func prepareRegistration(reg map[string]any, deviceID string) {
	delete(reg, "vowlan_secret")
	delete(reg, "notification_forwarding")
	reg["android_agent_id"] = deviceID
	reg["android_agent_kind"] = "mu300"
	reg["android_device_name"] = "AirSIM VIPME-M8"
	reg["android_manufacturer"] = "ZTE"
	reg["android_model"] = "VIPME-M8"
}
