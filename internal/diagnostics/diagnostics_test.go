package diagnostics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/platform"
)

func bptr(b bool) *bool       { return &b }
func fptr(f float64) *float64 { return &f }
func uptr(u uint64) *uint64   { return &u }
func iptr(i int) *int         { return &i }

func findingIDs(f []Finding) map[string]Finding {
	m := map[string]Finding{}
	for _, x := range f {
		m[x.ID] = x
	}
	return m
}

func TestDoctorFlagsPowerSaverOnAC(t *testing.T) {
	r := detect.Report{Power: detect.Power{OnAC: bptr(true), PPDProfile: "power-saver",
		Managers: []detect.Manager{{Name: "power-profiles-daemon", Active: true}}}}
	f := findingIDs(Diagnose(r))
	x, ok := f["power.saver-on-ac"]
	if !ok || x.Sev != SevWarn || x.Fix == nil || x.Fix.TweakID != "power.profile" || x.Fix.Params["profile"] != "balanced" {
		t.Errorf("finding: %+v", x)
	}
}

func TestDoctorSaysHealthySystemIsHealthy(t *testing.T) {
	r := detect.Report{
		Power:   detect.Power{OnAC: bptr(true), PPDProfile: "balanced", Managers: []detect.Manager{{Name: "power-profiles-daemon", Active: true}}},
		CPU:     detect.CPU{Driver: "intel_pstate", Governor: "powersave", TurboEnabled: bptr(true), ThrottleEvents: uptr(0), TempC: fptr(45)},
		Memory:  detect.Memory{TotalKB: 32 << 20, AvailableKB: 20 << 20, SwapTotalKB: 8 << 20, SwapFreeKB: 8 << 20, PSIMemSome: fptr(0), PSIMemFull: fptr(0)},
		Storage: []detect.Mount{{Path: "/", TotalBytes: 1 << 40, FreeBytes: 500 << 30}},
		Tools:   []detect.Tool{{Name: "gamemoderun", Present: true}, {Name: "mangohud", Present: true}},
		GPUs:    []detect.GPU{{Vendor: "amd", Driver: "amdgpu", PCIID: "1002:1", BootVGA: true}},
		Network: detect.Network{DefaultIface: "eth0", Gateway: "10.0.0.1", Resolvers: []string{"10.0.0.1"}},
	}
	w, n := Counts(Diagnose(r))
	if w != 0 || n != 0 {
		t.Errorf("a healthy machine produced %d warnings, %d notices: %+v", w, n, Diagnose(r))
	}
}

func TestDoctorRules(t *testing.T) {
	cases := []struct {
		name string
		r    detect.Report
		id   string
		sev  Severity
	}{
		{"two managers", detect.Report{Power: detect.Power{Managers: []detect.Manager{{Name: "TLP", Active: true}, {Name: "TuneD", Active: true}}}}, "power.conflict", SevWarn},
		{"turbo off", detect.Report{CPU: detect.CPU{TurboEnabled: bptr(false)}}, "cpu.turbo-off", SevWarn},
		{"thermal 10%", detect.Report{System: detect.System{UptimeSec: 36000}, CPU: detect.CPU{ThrottleSeconds: fptr(3600)}}, "cpu.thermal", SevNotice},
		{"thermal 30%", detect.Report{System: detect.System{UptimeSec: 36000}, CPU: detect.CPU{ThrottleSeconds: fptr(10800)}}, "cpu.thermal", SevWarn},
		{"thermal 1%", detect.Report{System: detect.System{UptimeSec: 36000}, CPU: detect.CPU{ThrottleSeconds: fptr(360)}}, "cpu.thermal", SevOK},
		{"mem stall", detect.Report{Memory: detect.Memory{TotalKB: 100, AvailableKB: 50, PSIMemFull: fptr(3)}}, "mem.pressure", SevWarn},
		{"disk full", detect.Report{Storage: []detect.Mount{{Path: "/", TotalBytes: 100 << 30, FreeBytes: 3 << 30}}}, "storage.full:/", SevWarn},
		{"nouveau", detect.Report{GPUs: []detect.GPU{{Vendor: "nvidia", Driver: "nouveau", PCIID: "10de:1"}}}, "gpu.nouveau", SevWarn},
		{"no resolver", detect.Report{Network: detect.Network{DefaultIface: "eth0"}}, "net.no-dns", SevWarn},
		{"100M ethernet", detect.Report{Network: detect.Network{Interfaces: []detect.Iface{{Name: "enp1", Kind: "ethernet", SpeedMbps: iptr(100)}}}}, "net.slow-ethernet:enp1", SevNotice},
		{"acpi powersave", detect.Report{Power: detect.Power{OnAC: bptr(true)}, CPU: detect.CPU{Driver: "acpi-cpufreq", Governor: "powersave"}}, "cpu.governor-powersave", SevNotice},
	}
	for _, c := range cases {
		got, ok := findingIDs(Diagnose(c.r))[c.id]
		if !ok || got.Sev != c.sev {
			t.Errorf("%s: want %s/%v, got %+v (found=%v)", c.name, c.id, c.sev, got, ok)
		}
	}
}

func TestDoctorDoesNotFlagIntelPstatePowersave(t *testing.T) {
	f := findingIDs(Diagnose(detect.Report{CPU: detect.CPU{Driver: "intel_pstate", Governor: "powersave"}}))
	if _, bad := f["cpu.governor-powersave"]; bad {
		t.Error("intel_pstate 'powersave' is normal and must not be flagged as a problem")
	}
	if x := f["cpu.governor-note"]; x.Sev != SevInfo {
		t.Errorf("expected an explanatory info finding, got %+v", x)
	}
}

func TestDoctorHybridGraphicsAdvice(t *testing.T) {
	f := findingIDs(Diagnose(detect.Report{GPUs: []detect.GPU{
		{Vendor: "intel", PCIID: "8086:a788", BootVGA: true},
		{Vendor: "nvidia", PCIID: "10de:2d58", Driver: "nvidia", Name: "GeForce RTX 5060"},
	}}))
	if x, ok := f["gpu.hybrid"]; !ok || !strings.Contains(x.Detail, "PRIME") {
		t.Errorf("%+v", x)
	}
}

func storageProblems(r detect.Report) int {
	n := 0
	for _, f := range Diagnose(r) {
		if f.Area == "Storage" && f.Sev >= SevNotice {
			n++
		}
	}
	return n
}

func TestSmallBootPartitionIsNotAlarming(t *testing.T) {
	// 548 MiB free of 1 GiB: an ordinary ESP, not a full disk.
	if n := storageProblems(detect.Report{Storage: []detect.Mount{{Path: "/boot", TotalBytes: 1 << 30, FreeBytes: 548 << 20}}}); n != 0 {
		t.Errorf("/boot flagged (%d)", n)
	}
	// The same absolute space on a small root filesystem is a real problem.
	if n := storageProblems(detect.Report{Storage: []detect.Mount{{Path: "/", TotalBytes: 2 << 30, FreeBytes: 100 << 20}}}); n != 1 {
		t.Error("nearly-full root not flagged")
	}
	// 1.5 GiB free of 1 TiB is 0.15%.
	if n := storageProblems(detect.Report{Storage: []detect.Mount{{Path: "/home", TotalBytes: 1 << 40, FreeBytes: 3 << 29}}}); n != 1 {
		t.Error("nearly-full home not flagged")
	}
}

func TestDoctorSortsMostSevereFirst(t *testing.T) {
	f := Diagnose(detect.Report{CPU: detect.CPU{TurboEnabled: bptr(false)}})
	for i := 1; i < len(f); i++ {
		if f[i].Sev > f[i-1].Sev {
			t.Fatalf("not sorted by severity: %+v", f)
		}
	}
}

// ---- metrics ----

func TestParseProcStatAndCPUPercent(t *testing.T) {
	fs := platform.NewMemFS()
	fs.Set("/proc/stat", "cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 1 1 1 1 1 1 1 1 1 1\n")
	fs.Set("/proc/meminfo", "MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 100 kB\nSwapFree: 100 kB\n")
	fs.Set("/proc/net/dev", "Inter-|   Receive\n face |bytes packets errs drop fifo frame compressed multicast|bytes\n  eth0: 1000 1 0 0 0 0 0 0 500 1 0 0 0 0 0 0\n    lo: 5 1 0 0 0 0 0 0 5 1 0 0 0 0 0 0\n")
	sm := &Sampler{FS: fs, Run: platform.NewFakeRunner(), NetIface: "eth0"}
	t0 := time.Unix(1000, 0)
	m := sm.Sample(context.Background(), t0)
	if m.HasRates || m.CPUPct != 0 {
		t.Error("first sample cannot have rates")
	}
	if m.MemUsedPct != 75 {
		t.Errorf("mem = %v", m.MemUsedPct)
	}
	// +100 jiffies of which 50 idle, +2000 rx bytes, +1000 tx over 2s
	fs.Set("/proc/stat", "cpu  125 0 125 850 0 0 0 0 0 0\n")
	fs.Set("/proc/net/dev", "x:\n  eth0: 3000 1 0 0 0 0 0 0 1500 1 0 0 0 0 0 0\n")
	m = sm.Sample(context.Background(), t0.Add(2*time.Second))
	if !m.HasRates || m.CPUPct != 50 {
		t.Errorf("cpu = %v (want 50)", m.CPUPct)
	}
	if m.NetRxBps != 1000 || m.NetTxBps != 500 {
		t.Errorf("net rates = %v %v", m.NetRxBps, m.NetTxBps)
	}
}

func TestSamplerDoesNotWakeSleepingNvidiaGPU(t *testing.T) {
	fs := platform.NewMemFS()
	fs.Set("/sys/bus/pci/devices/0000:01:00.0/power/runtime_status", "suspended\n")
	run := platform.NewFakeRunner().Install("nvidia-smi")
	sm := &Sampler{FS: fs, Run: run, GPUs: []detect.GPU{{Vendor: "nvidia", Driver: "nvidia", Slot: "0000:01:00.0", Name: "RTX"}}}
	m := sm.Sample(context.Background(), time.Now())
	if len(m.GPUs) != 1 || m.GPUs[0].Available || !strings.Contains(m.GPUs[0].Note, "asleep") {
		t.Errorf("%+v", m.GPUs)
	}
	if len(run.Calls) != 0 {
		t.Errorf("nvidia-smi was executed against a sleeping GPU: %v", run.Calls)
	}
	// Awake: query it.
	fs.Set("/sys/bus/pci/devices/0000:01:00.0/power/runtime_status", "active\n")
	run.On("nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits",
		platform.Reply{Stdout: "37, 1024, 8151, 55, 42.5\n"})
	m = sm.Sample(context.Background(), time.Now())
	if g := m.GPUs[0]; !g.Available || g.LoadPct != 37 || g.VRAMTotal != 8151 || g.PowerW != 42.5 {
		t.Errorf("%+v", g)
	}
}

func TestTopProcessesParsesCommWithSpacesAndParens(t *testing.T) {
	name, ticks, ok := parseStat("123 (Web Content (x)) S 1 1 1 0 -1 0 0 0 0 0 40 10 0 0 20 0 1 0 1 1 1")
	if !ok || name != "Web Content (x)" || ticks != 50 {
		t.Errorf("%q %d %v", name, ticks, ok)
	}
	if _, _, ok := parseStat("garbage"); ok {
		t.Error("garbage accepted")
	}
}

func TestTopProcessesRanksByCPU(t *testing.T) {
	fs := platform.NewMemFS()
	set := func(pid, name string, ticks int) {
		fs.Set("/proc/"+pid+"/stat", pid+" ("+name+") S 1 1 1 0 -1 0 0 0 0 0 "+itoa(ticks)+" 0 0 0 20 0 1 0 1 1 1")
		fs.Set("/proc/"+pid+"/statm", "100 2560 0 0 0 0 0")
	}
	set("10", "idle-ish", 5)
	set("20", "busy", 100)
	fs.Set("/proc/self/status", "x") // non-numeric entry must be ignored
	go func() {
		time.Sleep(20 * time.Millisecond)
		set("20", "busy", 160) // +60 ticks = 0.6 s CPU in the window
		set("10", "idle-ish", 6)
	}()
	top := TopProcesses(context.Background(), fs, 5, 100*time.Millisecond)
	if len(top) != 2 || top[0].Name != "busy" || top[0].CPUPct < 300 {
		t.Errorf("%+v", top)
	}
	if top[0].RSSMiB != 10 { // 2560 pages * 4 KiB
		t.Errorf("rss = %v", top[0].RSSMiB)
	}
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(i)) }
func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

// ---- network ----

func TestSummarizeLossAndJitter(t *testing.T) {
	s := Summarize("x", 10, []float64{10, 12, 10, 14, 10, 10, 10, 10})
	if s.Received != 8 || s.LossPct != 20 || s.MinMs != 10 || s.MaxMs != 14 {
		t.Errorf("%+v", s)
	}
	if s.JitterMs < 1.71 || s.JitterMs > 1.72 { // (2+2+4+4+0+0+0)/7
		t.Errorf("jitter = %v", s.JitterMs)
	}
	if z := Summarize("x", 5, nil); z.LossPct != 100 || z.Received != 0 {
		t.Errorf("all lost: %+v", z)
	}
}

type fakePinger struct {
	mu    sync.Mutex
	byTgt map[string]PingStats
	calls []string
	err   map[string]error
}

func (f *fakePinger) Ping(_ context.Context, host string, _ int, _ time.Duration) (PingStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, host)
	if e := f.err[host]; e != nil {
		return PingStats{}, e
	}
	s := f.byTgt[host]
	s.Target = host
	return s, nil
}

type fakeDNS struct {
	calls []string
	dur   time.Duration
	fail  bool
}

func (f *fakeDNS) Time(_ context.Context, server, name string) (time.Duration, error) {
	f.calls = append(f.calls, server+" "+name)
	if f.fail {
		return 0, errors.New("timeout")
	}
	return f.dur, nil
}

type fakeLoader struct {
	calls []string
	mbps  float64
}

func (f *fakeLoader) Load(ctx context.Context, u string, up bool) (float64, error) {
	f.calls = append(f.calls, u)
	<-ctx.Done()
	return f.mbps, nil
}

func netRep() detect.Report {
	return detect.Report{Network: detect.Network{DefaultIface: "wlan0", Gateway: "192.168.1.1", Resolvers: []string{"192.168.1.1"},
		Interfaces: []detect.Iface{{Name: "wlan0", Kind: "wifi"}}}}
}

func clean(rtt float64) PingStats {
	return Summarize("x", 20, []float64{rtt, rtt + 1, rtt, rtt + 1, rtt, rtt + 1, rtt, rtt + 1, rtt, rtt, rtt, rtt, rtt, rtt, rtt, rtt, rtt, rtt, rtt, rtt})
}

func TestNoExternalTrafficWithoutConsent(t *testing.T) {
	p := &fakePinger{byTgt: map[string]PingStats{"192.168.1.1": clean(2)}}
	d := &fakeDNS{dur: time.Millisecond}
	l := &fakeLoader{}
	r := CollectNetwork(context.Background(), netRep(), NetOptions{PingTarget: "1.1.1.1", LookupName: "example.com", LoadDownURL: "http://x/y"},
		NetDeps{Pinger: p, DNS: d, Loader: l}, nil)
	if len(p.calls) != 1 || p.calls[0] != "192.168.1.1" || len(d.calls) != 0 || len(l.calls) != 0 {
		t.Fatalf("contacted: pings=%v dns=%v load=%v", p.calls, d.calls, l.calls)
	}
	for _, dst := range r.Destinations {
		if dst.External {
			t.Errorf("external destination planned without consent: %+v", dst)
		}
	}
	if r.Internet != nil || len(r.DNS) != 0 {
		t.Error("external results present without consent")
	}
}

func TestConsentedRunContactsOnlyDisclosedDestinations(t *testing.T) {
	p := &fakePinger{byTgt: map[string]PingStats{"192.168.1.1": clean(2), "1.1.1.1": clean(15)}}
	d := &fakeDNS{dur: 12 * time.Millisecond}
	opt := NetOptions{External: true, PingTarget: "1.1.1.1", LookupName: "example.com"}
	rep := netRep()
	planned := map[string]bool{}
	for _, x := range PlanDestinations(rep, opt) {
		planned[x.Host] = true
	}
	r := CollectNetwork(context.Background(), rep, opt, NetDeps{Pinger: p, DNS: d}, nil)
	for _, c := range append(append([]string{}, p.calls...), "192.168.1.1") {
		if !planned[c] {
			t.Errorf("contacted %q which was not disclosed", c)
		}
	}
	if r.Internet == nil || len(r.DNS) != 1 || len(d.calls) != 5 {
		t.Errorf("%+v dnscalls=%d", r, len(d.calls))
	}
}

func hasDiag(d []Diagnosis, layer string, sev Severity) bool {
	for _, x := range d {
		if x.Layer == layer && x.Sev == sev {
			return true
		}
	}
	return false
}

func TestInterpretLocalWifiProblemIsNotBlamedOnTheISP(t *testing.T) {
	sig := -78
	r := NetReport{
		Link:     LinkInfo{Kind: "wifi", SignalDBm: &sig},
		Gateway:  ptrStats(Summarize("gw", 20, []float64{3, 40, 5, 60, 4, 80, 3, 5, 90, 4, 3, 4, 5, 3, 4, 3, 4, 5, 3, 4})),
		Internet: ptrStats(Summarize("1.1.1.1", 20, []float64{20, 60, 30, 90, 25, 100, 22, 30, 110, 25, 22, 25, 30, 22, 25, 22, 25, 30, 22, 25})),
	}
	d := Interpret(r)
	if !hasDiag(d, "Link", SevWarn) || !hasDiag(d, "LAN", SevWarn) {
		t.Errorf("want link+LAN warnings: %+v", d)
	}
	if hasDiag(d, "Internet", SevWarn) {
		t.Error("blamed the internet for a local Wi-Fi problem")
	}
	joined := ""
	for _, x := range d {
		joined += x.Detail
	}
	if !strings.Contains(joined, "cannot be fixed with a TCP/sysctl") {
		t.Error("should say local tweaks cannot fix it")
	}
}

func ptrStats(s PingStats) *PingStats { return &s }

func TestInterpretLossBeyondRouterBlamesISP(t *testing.T) {
	gw := clean(2)
	wan := Summarize("1.1.1.1", 20, []float64{20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20}) // 4 of 20 lost
	d := Interpret(NetReport{Link: LinkInfo{Kind: "ethernet"}, Gateway: &gw, Internet: &wan})
	if !hasDiag(d, "Internet", SevWarn) || hasDiag(d, "LAN", SevWarn) {
		t.Errorf("%+v", d)
	}
}

func TestInterpretBufferbloatPointsAtTheRouter(t *testing.T) {
	gw, idle := clean(2), clean(15)
	under := Summarize("1.1.1.1", 50, repeat(260, 50)) // p95 260 ms vs ~16 idle
	d := Interpret(NetReport{Link: LinkInfo{Kind: "ethernet"}, Gateway: &gw, Internet: &idle,
		Download: &LoadStats{Direction: "download", Mbps: 300, Under: under}})
	var bloat *Diagnosis
	for i := range d {
		if d[i].Layer == "Load" {
			bloat = &d[i]
		}
	}
	if bloat == nil || bloat.Sev != SevWarn || !strings.Contains(bloat.Detail, "SQM") || !strings.Contains(bloat.Detail, "Changing DNS") {
		t.Errorf("%+v", bloat)
	}
	// Steady latency under load -> OK, not bufferbloat.
	steady := Summarize("1.1.1.1", 50, repeat(16, 50))
	d = Interpret(NetReport{Internet: &idle, Download: &LoadStats{Direction: "download", Under: steady}})
	if !hasDiag(d, "Load", SevOK) {
		t.Errorf("%+v", d)
	}
}

func repeat(v float64, n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = v
	}
	return s
}

func TestInterpretSlowDNSDoesNotImplyGamePing(t *testing.T) {
	gw, wan := clean(2), clean(15)
	d := Interpret(NetReport{Gateway: &gw, Internet: &wan, DNS: []DNSStats{{Server: "192.168.1.1", Failures: 0, MedianMs: 300}}})
	var dns *Diagnosis
	for i := range d {
		if d[i].Layer == "DNS" {
			dns = &d[i]
		}
	}
	if dns == nil || dns.Sev != SevNotice || !strings.Contains(dns.Detail, "does not change in-game ping") {
		t.Errorf("%+v", dns)
	}
}

func TestInterpretAllHealthy(t *testing.T) {
	sig := -45
	gw, wan := clean(2), clean(15)
	d := Interpret(NetReport{Link: LinkInfo{Kind: "wifi", SignalDBm: &sig}, Gateway: &gw, Internet: &wan,
		DNS: []DNSStats{{Server: "x", MedianMs: 10}}})
	for _, x := range d {
		if x.Sev > SevOK {
			t.Errorf("healthy network flagged: %+v", x)
		}
	}
}

func TestPingErrorIsRecordedNotFatal(t *testing.T) {
	p := &fakePinger{byTgt: map[string]PingStats{"1.1.1.1": clean(15)}, err: map[string]error{"192.168.1.1": ErrNoICMP}}
	r := CollectNetwork(context.Background(), netRep(), NetOptions{External: true, PingTarget: "1.1.1.1", LookupName: "example.com"},
		NetDeps{Pinger: p, DNS: &fakeDNS{dur: time.Millisecond}}, nil)
	if r.Gateway != nil || r.Internet == nil || len(r.Skipped) == 0 {
		t.Errorf("one failing layer must not stop the others: %+v", r)
	}
}

func TestParseIWLink(t *testing.T) {
	var l LinkInfo
	ParseIWLink("Connected to aa:bb (on wlan0)\n\tSSID: Home Net\n\tfreq: 5240.0\n\tsignal: -39 dBm\n\trx bitrate: 1134.2 MBit/s 80MHz HE-MCS 11\n", &l)
	if l.SignalDBm == nil || *l.SignalDBm != -39 || l.SSID != "Home Net" || *l.FreqMHz != 5240 || *l.BitrateMbps != 1134.2 {
		t.Errorf("%+v", l)
	}
}

func TestICMPLoopbackPing(t *testing.T) {
	st, err := ICMPPinger{Timeout: time.Second}.Ping(context.Background(), "127.0.0.1", 3, 20*time.Millisecond)
	if errors.Is(err, ErrNoICMP) {
		t.Skip("unprivileged ICMP not permitted here: " + err.Error())
	}
	if err != nil {
		t.Fatal(err)
	}
	if st.Received != 3 || st.LossPct != 0 || st.MaxMs > 50 {
		t.Errorf("loopback ping: %+v", st)
	}
}

func TestLoaderRejectsNonHTTPURLs(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://x/y", "javascript:alert(1)", "not a url", ""} {
		if _, err := (HTTPLoader{}).Load(context.Background(), u, false); err == nil {
			t.Errorf("accepted %q", u)
		}
	}
}

func TestLoaderRefusesRedirectsToUndisclosedHosts(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed a redirect to another host") }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()
	// 127.0.0.1:port differs between the two servers, so this is a cross-host redirect.
	if _, err := (HTTPLoader{}).Load(context.Background(), srv.URL, false); err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Fatalf("want refusal, got %v", err)
	}
}

func TestLoaderTreatsHTTPErrorsAsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("not found ", 1000), http.StatusNotFound)
	}))
	defer srv.Close()
	for _, up := range []bool{false, true} {
		mbps, err := (HTTPLoader{}).Load(context.Background(), srv.URL, up)
		if err == nil || mbps != 0 {
			t.Errorf("upload=%v: an error page must not count as throughput: %v mbps, err %v", up, mbps, err)
		}
	}
}

func TestLoadSecondsIsBounded(t *testing.T) {
	for in, want := range map[int]int{-5: 10, 0: 10, 30: 30, 100000: MaxLoadSeconds} {
		if got := (NetOptions{LoadSeconds: in}).withDefaults().LoadSeconds; got != want {
			t.Errorf("LoadSeconds %d -> %d, want %d", in, got, want)
		}
	}
}
