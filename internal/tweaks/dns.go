package tweaks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// DNSResolver switches the IPv4 DNS servers of the active NetworkManager
// connection between "automatic" (whatever the router/ISP hands out) and a
// well-known public resolver. It is never recommended automatically: faster
// lookups are only worth it if Network Doctor shows slow DNS, and the choice
// of who sees your lookups is yours.
type DNSResolver struct{}

type dnsChoice struct {
	servers []string
	ignore  string // ipv4.ignore-auto-dns
}

var dnsChoices = map[string]dnsChoice{
	"automatic":  {nil, "no"},
	"cloudflare": {[]string{"1.1.1.1", "1.0.0.1"}, "yes"},
	"quad9":      {[]string{"9.9.9.9", "149.112.112.112"}, "yes"},
	"google":     {[]string{"8.8.8.8", "8.8.4.4"}, "yes"},
}

var dnsOptions = []Option{
	{"automatic", "Automatic (router / ISP)", "Use the DNS servers your network provides. The default."},
	{"cloudflare", "Cloudflare 1.1.1.1", "Public resolver run by Cloudflare. Your lookups go to Cloudflare."},
	{"quad9", "Quad9 9.9.9.9", "Public resolver run by Quad9; blocks known malicious domains."},
	{"google", "Google 8.8.8.8", "Public resolver run by Google. Your lookups go to Google."},
}

func (DNSResolver) Meta() Meta {
	return Meta{
		ID: "net.dns", Name: "DNS servers",
		Summary:  "Use a different DNS resolver on this connection if your router's or ISP's is slow.",
		Details:  "Sets ipv4.dns and ipv4.ignore-auto-dns on the active NetworkManager connection and re-applies it to the adapter. DNS decides how fast names like game servers or websites are looked up; it does not change the speed or ping of the connection itself or in-game latency. Only IPv4 servers are changed.",
		Tradeoff: "The resolver you pick sees every name you look up. A resolver closer to you can be faster, a farther one slower, and some networks (company, captive portal, local names) need their own DNS.",
		Evidence: "Network Doctor's DNS timings: compare first-lookup and median times before and after, over several runs.",
		Category: CatNetwork, Tier: TierSituational, Risk: RiskLow, Scope: ScopePersistent,
		Disruptive: true, ParamName: "resolver", Options: dnsOptions,
	}
}

func (DNSResolver) target(ctx context.Context, env Env) (wifiTarget, error) {
	iface := env.Report.Network.DefaultIface
	if iface == "" {
		return wifiTarget{}, errors.New("no default network connection")
	}
	res, err := env.Run.Run(ctx, "nmcli", "-t", "-f", "UUID,DEVICE", "connection", "show", "--active")
	if err != nil {
		return wifiTarget{}, fmt.Errorf("nmcli: %w", err)
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Split(strings.TrimSpace(line), ":")
		if len(f) == 2 && f[1] == iface && uuidRe.MatchString(f[0]) && ifaceRe.MatchString(f[1]) {
			return wifiTarget{UUID: f[0], Iface: f[1]}, nil
		}
	}
	return wifiTarget{}, errors.New("the default connection is not managed by NetworkManager")
}

func (d DNSResolver) Assess(ctx context.Context, env Env) Assessment {
	switch {
	case !env.Run.Has("nmcli"):
		return Assessment{Reason: "NetworkManager's nmcli is not installed."}
	case !env.Report.Network.NetworkManager:
		return Assessment{Reason: "NetworkManager is not running, so it is not managing your network."}
	}
	if _, err := d.target(ctx, env); err != nil {
		return Assessment{Reason: err.Error() + "."}
	}
	cur, err := d.Read(ctx, env, State{"ipv4.dns": "", "ipv4.ignore-auto-dns": ""})
	if err != nil {
		return Assessment{Reason: "Could not read the connection's settings: " + err.Error()}
	}
	return Assessment{Available: true, Current: cur, Options: dnsOptions}
}

// normalizeDNS turns nmcli's "1.1.1.1, 1.0.0.1" / "1.1.1.1,1.0.0.1" / "" into
// a canonical comma list and drops anything that is not an IP address.
func normalizeDNS(s string) string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if ip := net.ParseIP(f); ip != nil {
			out = append(out, ip.String())
		}
	}
	return strings.Join(out, ",")
}

// Summary shows "automatic" or the servers in use.
func (DNSResolver) Summary(s State) string {
	switch {
	case s["ipv4.dns"] == "":
		return "automatic"
	case s["ipv4.ignore-auto-dns"] == "yes":
		return s["ipv4.dns"]
	}
	return s["ipv4.dns"] + " + router"
}

func (d DNSResolver) Read(ctx context.Context, env Env, _ State) (State, error) {
	t, err := d.target(ctx, env)
	if err != nil {
		return nil, err
	}
	res, err := env.Run.Run(ctx, "nmcli", "-g", "ipv4.dns,ipv4.ignore-auto-dns", "connection", "show", t.UUID)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n")
	st := State{"ipv4.dns": "", "ipv4.ignore-auto-dns": "no"}
	if len(lines) > 0 {
		st["ipv4.dns"] = normalizeDNS(lines[0])
	}
	if len(lines) > 1 && strings.HasPrefix(strings.TrimSpace(strings.ToLower(lines[1])), "y") {
		st["ipv4.ignore-auto-dns"] = "yes"
	}
	return st, nil
}

func (d DNSResolver) Plan(ctx context.Context, env Env, params map[string]string) (Plan, error) {
	m := d.Meta()
	v := params["resolver"]
	if err := CheckParam(m, nil, v); err != nil {
		return Plan{}, err
	}
	a := d.Assess(ctx, env)
	if !a.Available {
		return Plan{}, fmt.Errorf("%w: %s", ErrUnavailable, a.Reason)
	}
	c := dnsChoices[v]
	after := State{"ipv4.dns": strings.Join(c.servers, ","), "ipv4.ignore-auto-dns": c.ignore}
	if a.Current.Covers(after) {
		return Plan{}, ErrNoChange
	}
	show := func(s State) string {
		if s["ipv4.dns"] == "" {
			return "automatic"
		}
		return s["ipv4.dns"]
	}
	pl := Plan{
		TweakID: m.ID, Params: map[string]string{"resolver": v},
		Before: a.Current.Clone(), After: after,
		Changes:    []Change{{"DNS servers (IPv4)", show(a.Current), show(after)}},
		Disruptive: true,
	}
	if len(c.servers) > 0 {
		pl.Warnings = append(pl.Warnings, "Every name you look up will be sent to "+v+" instead of your router/ISP.")
	}
	pl.Warnings = append(pl.Warnings, "The change is re-applied live; names are briefly unresolved while it happens. A NetworkManager checkpoint undoes it automatically if connectivity breaks.")
	return pl, nil
}

// set writes the profile. Every address is parsed again here, so a damaged
// journal can never put anything but IP addresses on a command line.
func (d DNSResolver) set(ctx context.Context, env Env, s State, strict bool) error {
	var servers []string
	for _, f := range strings.Split(s["ipv4.dns"], ",") {
		if f == "" {
			continue
		}
		ip := net.ParseIP(f)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("%w: %q is not an IPv4 address", ErrInvalidParam, f)
		}
		servers = append(servers, ip.String())
	}
	ignore := s["ipv4.ignore-auto-dns"]
	if ignore != "yes" && ignore != "no" {
		return fmt.Errorf("%w: ipv4.ignore-auto-dns %q", ErrInvalidParam, ignore)
	}
	t, err := d.target(ctx, env)
	if err != nil {
		return err
	}
	if _, err := env.Run.Run(ctx, "nmcli", "connection", "modify", t.UUID, "ipv4.dns", strings.Join(servers, " "), "ipv4.ignore-auto-dns", ignore); err != nil {
		return fmt.Errorf("nmcli connection modify: %w", err)
	}
	if _, err := env.Run.Run(ctx, "nmcli", "device", "reapply", t.Iface); err != nil && strict {
		return fmt.Errorf("nmcli device reapply: %w", err)
	}
	return nil
}

func (d DNSResolver) Apply(ctx context.Context, env Env, p Plan) error {
	return d.set(ctx, env, p.After, true)
}

func (d DNSResolver) Restore(ctx context.Context, env Env, p Plan) error {
	return d.set(ctx, env, p.Before, false)
}

// BeginGuard reuses the Wi-Fi tweak's NetworkManager checkpoint.
func (DNSResolver) BeginGuard(ctx context.Context, env Env, p Plan) (Guard, error) {
	return WifiPowerSave{}.BeginGuard(ctx, env, p)
}
