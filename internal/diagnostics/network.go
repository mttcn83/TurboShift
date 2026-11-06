package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/stats"
)

// NetOptions configures a Network Doctor run.
type NetOptions struct {
	// External is the user's explicit consent to contact hosts beyond the
	// local network (ping target, DNS lookups, load URLs). Without it only
	// the local link and the default gateway are tested.
	External    bool
	PingTarget  string
	LookupName  string
	LoadDownURL string // optional: user-chosen file to download while measuring latency
	LoadUpURL   string // optional: user-chosen endpoint that accepts an HTTP POST body
	Count       int    // probes per ping run (default 20)
	Interval    time.Duration
	LoadSeconds int
}

// MaxLoadSeconds bounds a load test so a typo cannot saturate the link for hours.
const MaxLoadSeconds = 120

func (o NetOptions) withDefaults() NetOptions {
	if o.Count == 0 {
		o.Count = 20
	}
	if o.Interval == 0 {
		o.Interval = 200 * time.Millisecond
	}
	if o.LoadSeconds <= 0 {
		o.LoadSeconds = 10
	}
	o.LoadSeconds = min(o.LoadSeconds, MaxLoadSeconds)
	return o
}

// Destination is one network target a run would contact, shown to the user
// before anything is sent.
type Destination struct {
	Host     string `json:"host"`
	Purpose  string `json:"purpose"`
	External bool   `json:"external"`
}

// PlanDestinations lists exactly what Collect would contact for these options.
func PlanDestinations(rep detect.Report, o NetOptions) []Destination {
	o = o.withDefaults()
	var d []Destination
	if rep.Network.Gateway != "" {
		d = append(d, Destination{rep.Network.Gateway, "your router (default gateway): ping — local network only", false})
	}
	if !o.External {
		return d
	}
	for _, r := range limit(rep.Network.Resolvers, 2) {
		d = append(d, Destination{r, fmt.Sprintf("your DNS resolver: lookups of %q (the resolver forwards them upstream)", o.LookupName), true})
	}
	d = append(d, Destination{o.PingTarget, "ping — packet loss, latency, jitter to the internet", true})
	if o.LoadDownURL != "" {
		d = append(d, Destination{hostOf(o.LoadDownURL), fmt.Sprintf("download test for ~%ds (uses bandwidth) while pinging %s", o.LoadSeconds, o.PingTarget), true})
	}
	if o.LoadUpURL != "" {
		d = append(d, Destination{hostOf(o.LoadUpURL), fmt.Sprintf("upload test for ~%ds (uses bandwidth) while pinging %s", o.LoadSeconds, o.PingTarget), true})
	}
	return d
}

func limit(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return u
}

// ---- result types ----

type LinkInfo struct {
	Kind        string   `json:"kind"` // wifi | ethernet | none
	Iface       string   `json:"iface"`
	SignalDBm   *int     `json:"signal_dbm,omitempty"`
	BitrateMbps *float64 `json:"bitrate_mbps,omitempty"`
	FreqMHz     *float64 `json:"freq_mhz,omitempty"`
	SpeedMbps   *int     `json:"speed_mbps,omitempty"`
	PowerSave   string   `json:"power_save,omitempty"`
	Errors      uint64   `json:"interface_errors"`
	SSID        string   `json:"ssid,omitempty"` // shown in the UI; omitted from JSON by the CLI unless asked
}

type DNSStats struct {
	Server   string    `json:"server"`
	Name     string    `json:"name"`
	Lookups  []float64 `json:"lookups_ms"`
	Failures int       `json:"failures"`
	FirstMs  float64   `json:"first_ms"`
	MedianMs float64   `json:"median_ms"`
}

type LoadStats struct {
	Direction string    `json:"direction"` // download | upload
	Host      string    `json:"host"`
	Mbps      float64   `json:"mbps"`
	Under     PingStats `json:"latency_under_load"`
	Error     string    `json:"error,omitempty"`
}

type Diagnosis struct {
	Layer     string   `json:"layer"` // Link | LAN | DNS | Internet | Load | Device
	Sev       Severity `json:"-"`
	Severity  string   `json:"severity"`
	Certainty string   `json:"certainty"` // high | medium | low
	Title     string   `json:"title"`
	Detail    string   `json:"detail"`
}

type NetReport struct {
	Link         LinkInfo      `json:"link"`
	Gateway      *PingStats    `json:"gateway,omitempty"`
	DNS          []DNSStats    `json:"dns,omitempty"`
	Internet     *PingStats    `json:"internet,omitempty"`
	Download     *LoadStats    `json:"download,omitempty"`
	Upload       *LoadStats    `json:"upload,omitempty"`
	Destinations []Destination `json:"destinations"`
	Skipped      []string      `json:"skipped,omitempty"`
	Diagnosis    []Diagnosis   `json:"diagnosis"`
}

// ---- collection ----

type DNSTimer interface {
	Time(ctx context.Context, server, name string) (time.Duration, error)
}

type Loader interface {
	// Load generates sustained traffic until ctx ends and returns the average Mbit/s.
	Load(ctx context.Context, url string, upload bool) (mbps float64, err error)
}

type NetDeps struct {
	Pinger Pinger
	DNS    DNSTimer
	Loader Loader
	FS     platform.FS
	Run    platform.Runner
}

// RealNetDeps wires the real implementations.
func RealNetDeps(fs platform.FS, run platform.Runner) NetDeps {
	return NetDeps{Pinger: ICMPPinger{}, DNS: UDPResolver{}, Loader: HTTPLoader{}, FS: fs, Run: run}
}

// CollectNetwork runs the layered measurements. progress receives short
// status lines for the UI; it may be nil. Any step that fails is recorded in
// Skipped and the remaining layers still run.
func CollectNetwork(ctx context.Context, rep detect.Report, o NetOptions, d NetDeps, progress func(string)) NetReport {
	o = o.withDefaults()
	say := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	nr := NetReport{Destinations: PlanDestinations(rep, o)}

	say("Checking the link")
	nr.Link = collectLink(ctx, rep, d)

	if gw := rep.Network.Gateway; gw != "" {
		say("Pinging your router " + gw)
		st, err := d.Pinger.Ping(ctx, gw, o.Count, o.Interval)
		if err != nil {
			nr.Skipped = append(nr.Skipped, "router ping: "+err.Error())
		} else {
			nr.Gateway = &st
		}
	} else {
		nr.Skipped = append(nr.Skipped, "router ping: no default route")
	}

	if !o.External {
		nr.Skipped = append(nr.Skipped, "internet, DNS and load tests: not run (external tests need your explicit OK)")
		nr.Diagnosis = Interpret(nr)
		return nr
	}

	for _, srv := range limit(rep.Network.Resolvers, 2) {
		say("Timing DNS lookups via " + srv)
		nr.DNS = append(nr.DNS, timeDNS(ctx, d.DNS, srv, o.LookupName))
	}

	say("Pinging " + o.PingTarget)
	st, err := d.Pinger.Ping(ctx, o.PingTarget, o.Count, o.Interval)
	if err != nil {
		nr.Skipped = append(nr.Skipped, "internet ping: "+err.Error())
	} else {
		nr.Internet = &st
	}

	if nr.Internet != nil {
		if o.LoadDownURL != "" {
			say("Measuring latency under download load")
			nr.Download = loaded(ctx, d, o, o.LoadDownURL, false)
		}
		if o.LoadUpURL != "" {
			say("Measuring latency under upload load")
			nr.Upload = loaded(ctx, d, o, o.LoadUpURL, true)
		}
	}
	if o.LoadDownURL == "" && o.LoadUpURL == "" {
		nr.Skipped = append(nr.Skipped, "latency under load (bufferbloat): not run — it needs a test file/endpoint you choose, because TurboShift runs no server of its own")
	}
	nr.Diagnosis = Interpret(nr)
	return nr
}

func timeDNS(ctx context.Context, t DNSTimer, server, name string) DNSStats {
	s := DNSStats{Server: server, Name: name}
	var ok []float64
	for i := 0; i < 5 && ctx.Err() == nil; i++ {
		dur, err := t.Time(ctx, server, name)
		if err != nil {
			s.Failures++
			continue
		}
		ms := float64(dur.Microseconds()) / 1000
		s.Lookups = append(s.Lookups, ms)
		ok = append(ok, ms)
	}
	if len(ok) > 0 {
		s.FirstMs, s.MedianMs = ok[0], stats.Median(ok)
	}
	return s
}

func loaded(ctx context.Context, d NetDeps, o NetOptions, rawURL string, upload bool) *LoadStats {
	dir := "download"
	if upload {
		dir = "upload"
	}
	ls := &LoadStats{Direction: dir, Host: hostOf(rawURL)}
	lctx, cancel := context.WithTimeout(ctx, time.Duration(o.LoadSeconds+2)*time.Second)
	defer cancel()
	type res struct {
		mbps float64
		err  error
	}
	ch := make(chan res, 1)
	go func() { m, err := d.Loader.Load(lctx, rawURL, upload); ch <- res{m, err} }()
	// let the transfer ramp up before measuring (TCP slow start)
	select {
	case <-time.After(1500 * time.Millisecond):
	case <-ctx.Done():
	}
	count := int(time.Duration(o.LoadSeconds) * time.Second / o.Interval)
	st, err := d.Pinger.Ping(ctx, o.PingTarget, count, o.Interval)
	cancel()
	r := <-ch
	ls.Mbps = r.mbps
	if err != nil {
		ls.Error = "ping under load: " + err.Error()
	} else if r.err != nil && r.mbps == 0 {
		ls.Error = "transfer: " + r.err.Error()
	}
	ls.Under = st
	return ls
}

var (
	reSignal = regexp.MustCompile(`signal:\s*(-?\d+)\s*dBm`)
	reRate   = regexp.MustCompile(`(?:rx|tx) bitrate:\s*([\d.]+)\s*MBit/s`)
	reFreq   = regexp.MustCompile(`freq:\s*([\d.]+)`)
	reSSID   = regexp.MustCompile(`SSID:\s*(.*)`)
)

// ParseIWLink parses `iw dev X link`. SSID may be empty if not associated.
func ParseIWLink(s string, l *LinkInfo) {
	if m := reSignal.FindStringSubmatch(s); m != nil {
		v, _ := strconv.Atoi(m[1])
		l.SignalDBm = &v
	}
	if m := reRate.FindStringSubmatch(s); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		l.BitrateMbps = &v
	}
	if m := reFreq.FindStringSubmatch(s); m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		l.FreqMHz = &v
	}
	if m := reSSID.FindStringSubmatch(s); m != nil {
		l.SSID = strings.TrimSpace(m[1])
	}
}

func collectLink(ctx context.Context, rep detect.Report, d NetDeps) LinkInfo {
	l := LinkInfo{Kind: "none", Iface: rep.Network.DefaultIface, PowerSave: rep.Network.WifiPowerSave}
	for _, i := range rep.Network.Interfaces {
		if i.Name != l.Iface {
			continue
		}
		l.Kind, l.SpeedMbps, l.Errors = i.Kind, i.SpeedMbps, i.RxErrors+i.TxErrors
		if i.Kind == "wifi" && d.Run != nil && d.Run.Has("iw") {
			if res, err := d.Run.Run(ctx, "iw", "dev", i.Name, "link"); err == nil {
				ParseIWLink(res.Stdout, &l)
			}
		}
	}
	return l
}

// ---- interpretation ----

// Interpret turns measurements into layer-attributed conclusions. It is pure
// and deterministic; thresholds are deliberately conservative and every
// conclusion carries a certainty, because short ICMP runs have limits.
func Interpret(r NetReport) []Diagnosis {
	var out []Diagnosis
	add := func(layer string, sev Severity, cert, title string, detail ...string) {
		out = append(out, Diagnosis{Layer: layer, Sev: sev, Severity: sev.String(), Certainty: cert, Title: title, Detail: strings.Join(detail, " ")})
	}
	wifi := r.Link.Kind == "wifi"

	// Link layer
	if wifi && r.Link.SignalDBm != nil {
		s := *r.Link.SignalDBm
		switch {
		case s < -75:
			add("Link", SevWarn, "high", fmt.Sprintf("Weak Wi-Fi signal (%d dBm)", s),
				"Below about -75 dBm, retransmissions cause loss, jitter and low speeds. Move closer to the router, use the 5 GHz band if the router is near, or use Ethernet. No software setting fixes a weak radio link.")
		case s < -67:
			add("Link", SevNotice, "medium", fmt.Sprintf("Fair Wi-Fi signal (%d dBm)", s),
				"Usable, but fine for browsing more than for latency-sensitive games. Expect more variation than on a strong signal.")
		default:
			add("Link", SevOK, "high", fmt.Sprintf("Strong Wi-Fi signal (%d dBm)", s))
		}
	}
	if r.Link.Errors >= 100 {
		add("Link", SevNotice, "medium", fmt.Sprintf("%d packet errors on %s since boot", r.Link.Errors, r.Link.Iface),
			"Errors on a physical link point at cabling, the port, or the driver.")
	}
	if r.Link.Kind == "ethernet" && r.Link.SpeedMbps != nil && *r.Link.SpeedMbps <= 100 {
		add("Link", SevNotice, "medium", fmt.Sprintf("Ethernet negotiated %d Mbit/s", *r.Link.SpeedMbps),
			"Gigabit hardware falls back to 100 Mbit/s with a damaged cable or poor connector.")
	}

	// LAN layer
	gwBad := false
	if g := r.Gateway; g != nil {
		switch {
		case g.Received == 0:
			gwBad = true
			add("LAN", SevWarn, "medium", "Router did not answer any pings",
				"Either the router blocks ICMP (some do) or the local link is down. If the internet works, the router probably just ignores pings.")
		case g.LossPct > 1 || g.JitterMs > 10 || g.P95Ms > 30:
			gwBad = true
			where := "the cable/port, the router, or another device saturating the LAN"
			if wifi {
				where = "your Wi-Fi link or the router (interference, distance, or a busy channel)"
			}
			add("LAN", SevWarn, "medium",
				fmt.Sprintf("Unstable path to your own router (loss %.1f%%, jitter %.1f ms, p95 %.1f ms)", g.LossPct, g.JitterMs, g.P95Ms),
				"The problem is between this computer and the router, so it is local: "+where+". This cannot be fixed with a TCP/sysctl tweak, and an internet-side change will not help.")
		default:
			add("LAN", SevOK, "medium", fmt.Sprintf("Router path is clean (loss %.1f%%, jitter %.1f ms)", g.LossPct, g.JitterMs))
		}
	}

	// DNS layer
	for _, d := range r.DNS {
		switch {
		case d.Failures == 5:
			add("DNS", SevWarn, "high", "DNS resolver "+d.Server+" failed every lookup",
				"Names cannot be resolved through it. If pings to an IP address work, the resolver (not the connection) is the problem — try another resolver in your network settings.")
		case d.Failures > 0:
			add("DNS", SevNotice, "medium", fmt.Sprintf("DNS resolver %s failed %d of 5 lookups", d.Server, d.Failures), "Intermittent resolver failures show up as pages and game services that hang before connecting.")
		case d.MedianMs > 100:
			add("DNS", SevNotice, "medium", fmt.Sprintf("Slow DNS via %s (median %.0f ms)", d.Server, d.MedianMs),
				"A slow resolver delays the first connection to a site or server. It does not change in-game ping or FPS once connected.")
		default:
			add("DNS", SevOK, "medium", fmt.Sprintf("DNS via %s is fast (median %.0f ms)", d.Server, d.MedianMs))
		}
	}

	// Internet layer
	if w := r.Internet; w != nil {
		switch {
		case w.Received == 0:
			add("Internet", SevWarn, "low", "No replies from "+w.Target, "Some networks and hosts drop ICMP. Try a different target before concluding the internet is down.")
		case w.LossPct > 1 && !gwBad && r.Gateway != nil:
			add("Internet", SevWarn, "medium", fmt.Sprintf("Packet loss beyond your router (%.1f%% to %s)", w.LossPct, w.Target),
				"The router path is clean, so loss happens further out: your ISP or the route to that host. Only your ISP/router settings can change that; local Linux tweaks cannot.")
		case w.JitterMs > 20 && !gwBad && r.Gateway != nil:
			add("Internet", SevNotice, "medium", fmt.Sprintf("Variable latency beyond your router (jitter %.1f ms)", w.JitterMs),
				"Your local link is steady, so the variation comes from the ISP or route (or from load on the router itself).")
		case gwBad:
			add("Internet", SevInfo, "medium", "Internet results are affected by the local problem above",
				"Fix the router path first; measurements to the internet include that unstable hop.")
		default:
			add("Internet", SevOK, "medium", fmt.Sprintf("Internet path is healthy (%.0f ms median, loss %.1f%%, jitter %.1f ms)", w.P50Ms, w.LossPct, w.JitterMs))
		}
	}

	// Load layer: compare against the idle internet baseline.
	for _, ls := range []*LoadStats{r.Download, r.Upload} {
		if ls == nil {
			continue
		}
		if ls.Error != "" {
			add("Load", SevInfo, "low", fmt.Sprintf("%s load test did not complete", ls.Direction), ls.Error)
			continue
		}
		if r.Internet == nil || ls.Under.Received < 10 {
			add("Load", SevInfo, "low", ls.Direction+" load test had too few replies to judge", "Run it again, or use a longer test.")
			continue
		}
		extra := ls.Under.P95Ms - r.Internet.P95Ms
		desc := fmt.Sprintf("p95 latency rose from %.0f ms idle to %.0f ms during %s at %.0f Mbit/s", r.Internet.P95Ms, ls.Under.P95Ms, ls.Direction, ls.Mbps)
		fix := "The queue that fills up is almost always in the router/modem. The fix is router-side smart queue management (SQM with CAKE or fq_codel) or limiting bandwidth slightly below your line rate. Changing DNS or a local TCP setting will not solve it."
		switch {
		case extra > 200:
			add("Load", SevWarn, "medium", "Severe bufferbloat during "+ls.Direction, desc+". "+fix)
		case extra > 60:
			add("Load", SevWarn, "medium", "Noticeable bufferbloat during "+ls.Direction, desc+". "+fix)
		case extra > 30:
			add("Load", SevNotice, "low", "Mild latency increase during "+ls.Direction, desc+". Borderline; repeat the test to confirm.")
		default:
			add("Load", SevOK, "medium", "Latency stays steady during "+ls.Direction, desc+".")
		}
	}

	if len(out) == 0 {
		add("Link", SevInfo, "low", "Not enough data to judge", "No layer produced measurements.")
	}
	return out
}

// ---- real DNS timer and loader ----

// UDPResolver times one lookup against one specific DNS server over UDP/53.
type UDPResolver struct{}

func (UDPResolver) Time(ctx context.Context, server, name string) (time.Duration, error) {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "udp", net.JoinHostPort(server, "53"))
	}}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	t0 := time.Now()
	_, err := r.LookupHost(cctx, name)
	return time.Since(t0), err
}

// HTTPLoader generates load against a user-chosen URL.
type HTTPLoader struct{}

// countingZeros yields zero bytes until ctx ends and counts what was read,
// which is what the HTTP transport actually sent.
type countingZeros struct {
	ctx context.Context
	n   atomic.Int64
}

func (z *countingZeros) Read(p []byte) (int, error) {
	if z.ctx.Err() != nil {
		return 0, io.EOF
	}
	clear(p)
	z.n.Add(int64(len(p)))
	return len(p), nil
}

// sameHostClient follows redirects only within the host the user was shown in
// the "this run will contact" list, so a redirect cannot send traffic anywhere
// that was not disclosed.
var sameHostClient = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("refusing redirect to %s: only %s was disclosed", req.URL.Host, via[0].URL.Host)
	}
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	return nil
}}

func (HTTPLoader) Load(ctx context.Context, rawURL string, upload bool) (float64, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return 0, fmt.Errorf("invalid URL %q (need http:// or https://)", rawURL)
	}
	var req *http.Request
	var sent *countingZeros
	if upload {
		sent = &countingZeros{ctx: ctx}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, rawURL, sent)
		if req != nil {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	}
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "turboshift-network-doctor")
	t0 := time.Now()
	resp, err := sameHostClient.Do(req)
	var n int64
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 400 { // an error page is not a measurement
			return 0, fmt.Errorf("server answered %s", resp.Status)
		}
		if upload {
			_, _ = io.Copy(io.Discard, resp.Body)
		} else {
			n, _ = io.Copy(io.Discard, resp.Body)
		}
	}
	if upload {
		n = sent.n.Load()
	}
	secs := time.Since(t0).Seconds()
	if err != nil && n == 0 {
		if ctx.Err() != nil {
			return 0, nil // we stopped it ourselves
		}
		return 0, err
	}
	if secs <= 0 {
		return 0, nil
	}
	return float64(n) * 8 / secs / 1e6, nil
}
