package detect

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
)

func probeNetwork(ctx context.Context, e Env) Network {
	var n Network
	ents, _ := e.FS.ReadDir("/sys/class/net")
	for _, d := range ents {
		n.Interfaces = append(n.Interfaces, probeIface(e, d.Name()))
	}
	sort.Slice(n.Interfaces, func(i, j int) bool { return n.Interfaces[i].Name < n.Interfaces[j].Name })

	if b, err := e.FS.ReadFile("/proc/net/route"); err == nil {
		n.DefaultIface, n.Gateway = ParseDefaultRoute(string(b))
	}
	n.Resolvers, n.ResolverMode = probeResolvers(e)
	if e.Run.Has("systemctl") {
		res, _ := e.Run.Run(ctx, "systemctl", "is-active", "NetworkManager.service")
		n.NetworkManager = strings.TrimSpace(res.Stdout) == "active"
	}
	if n.DefaultIface != "" && e.Run.Has("iw") {
		for _, i := range n.Interfaces {
			if i.Name == n.DefaultIface && i.Kind == "wifi" {
				if res, err := e.Run.Run(ctx, "iw", "dev", i.Name, "get", "power_save"); err == nil {
					n.WifiPowerSave = ParseIWPowerSave(res.Stdout)
				}
			}
		}
	}
	return n
}

func probeIface(e Env, name string) Iface {
	base := "/sys/class/net/" + name
	i := Iface{Name: name}
	i.State, _ = platform.ReadTrim(e.FS, base+"/operstate")
	if v, ok := platform.ReadTrim(e.FS, base+"/carrier"); ok {
		c := v == "1"
		i.Carrier = &c
	}
	if v, ok := platform.ReadTrim(e.FS, base+"/speed"); ok {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			i.SpeedMbps = &s
		}
	}
	_, hasDev := linkTarget(e, base+"/device")
	_, wireless := e.FS.Stat(base + "/wireless")
	_, phy := e.FS.Stat(base + "/phy80211")
	switch {
	case name == "lo":
		i.Kind = "loopback"
	case wireless == nil || phy == nil:
		i.Kind = "wifi"
	case hasDev:
		i.Kind = "ethernet"
	default:
		i.Kind = "virtual"
	}
	if t, ok := linkTarget(e, base+"/device/driver"); ok {
		i.Driver = t[strings.LastIndex(t, "/")+1:]
	}
	stat := func(f string) uint64 {
		v, _ := platform.ReadTrim(e.FS, base+"/statistics/"+f)
		x, _ := strconv.ParseUint(v, 10, 64)
		return x
	}
	i.RxErrors, i.TxErrors = stat("rx_errors"), stat("tx_errors")
	i.RxDropped, i.TxDropped = stat("rx_dropped"), stat("tx_dropped")
	return i
}

func linkTarget(e Env, p string) (string, bool) {
	t, err := e.FS.Readlink(p)
	return t, err == nil
}

// ParseDefaultRoute reads /proc/net/route and returns the lowest-metric
// default route's interface and gateway (IPv4).
func ParseDefaultRoute(s string) (iface, gw string) {
	bestMetric := -1
	for i, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 8 || f[1] != "00000000" {
			continue
		}
		flags, _ := strconv.ParseUint(f[3], 16, 32)
		if flags&0x1 == 0 { // RTF_UP
			continue
		}
		metric, _ := strconv.Atoi(f[6])
		if bestMetric >= 0 && metric >= bestMetric {
			continue
		}
		b, err := hex.DecodeString(f[2])
		if err != nil || len(b) != 4 {
			continue
		}
		bestMetric, iface = metric, f[0]
		gw = net.IPv4(b[3], b[2], b[1], b[0]).String() // kernel prints little-endian
	}
	return iface, gw
}

func probeResolvers(e Env) ([]string, string) {
	b, err := e.FS.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil, "unknown"
	}
	ns := ParseResolvConf(string(b))
	mode := "static"
	if t, ok := linkTarget(e, "/etc/resolv.conf"); ok && strings.Contains(t, "systemd/resolve") {
		mode = "systemd-resolved"
	}
	for _, s := range ns {
		if s == "127.0.0.53" {
			mode = "systemd-resolved"
		}
	}
	return ns, mode
}

// ParseResolvConf returns the nameserver addresses in resolv.conf order.
func ParseResolvConf(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "nameserver" {
			out = append(out, f[1])
		}
	}
	return out
}

// ParseIWPowerSave parses `iw dev X get power_save` ("Power save: on").
func ParseIWPowerSave(s string) string {
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "power save") {
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return ""
}

// FormatBytes renders a byte count with a binary unit.
func FormatBytes(b uint64) string {
	const u = 1024.0
	f := float64(b)
	for _, s := range []string{"B", "KiB", "MiB", "GiB", "TiB"} {
		if f < u || s == "TiB" {
			if s == "B" {
				return fmt.Sprintf("%d B", b)
			}
			return fmt.Sprintf("%.1f %s", f, s)
		}
		f /= u
	}
	return ""
}
