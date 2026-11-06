package detect

import (
	"bufio"
	"context"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
)

func probeSystem(e Env) System {
	s := System{Arch: e.GOARCH}
	if b, err := e.FS.ReadFile("/etc/os-release"); err == nil {
		kv := parseKV(string(b))
		s.Distro = kv["PRETTY_NAME"]
		if s.Distro == "" {
			s.Distro = kv["NAME"]
		}
		s.DistroID, s.DistroLike = kv["ID"], kv["ID_LIKE"]
	}
	s.Kernel, _ = platform.ReadTrim(e.FS, "/proc/sys/kernel/osrelease")
	if up, ok := platform.ReadTrim(e.FS, "/proc/uptime"); ok {
		if f, _, _ := strings.Cut(up, " "); f != "" {
			if v, err := strconv.ParseFloat(f, 64); err == nil {
				s.UptimeSec = int64(v)
			}
		}
	}
	if e.Getenv != nil {
		s.SessionType = e.Getenv("XDG_SESSION_TYPE")
		s.Desktop = e.Getenv("XDG_CURRENT_DESKTOP")
	}
	return s
}

// parseKV parses shell-style KEY="value" lines (os-release).
func parseKV(s string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[k] = strings.Trim(v, `"'`)
	}
	return out
}

func probeMemory(e Env) Memory {
	var m Memory
	if b, err := e.FS.ReadFile("/proc/meminfo"); err == nil {
		kv := parseMeminfo(string(b))
		m.TotalKB, m.AvailableKB = kv["MemTotal"], kv["MemAvailable"]
		m.SwapTotalKB, m.SwapFreeKB = kv["SwapTotal"], kv["SwapFree"]
	}
	if ents, err := e.FS.ReadDir("/sys/block"); err == nil {
		for _, d := range ents {
			if strings.HasPrefix(d.Name(), "zram") {
				m.ZramDevices++
			}
		}
	}
	m.PSIMemSome = psiAvg10(e, "/proc/pressure/memory", "some")
	m.PSIMemFull = psiAvg10(e, "/proc/pressure/memory", "full")
	m.PSIIOSome = psiAvg10(e, "/proc/pressure/io", "some")
	m.PSICPUSome = psiAvg10(e, "/proc/pressure/cpu", "some")
	return m
}

func parseMeminfo(s string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(s, "\n") {
		k, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		if v, err := strconv.ParseUint(f[0], 10, 64); err == nil {
			out[k] = v
		}
	}
	return out
}

// psiAvg10 reads the avg10 figure from a pressure-stall-information file.
func psiAvg10(e Env, file, kind string) *float64 {
	b, err := e.FS.ReadFile(file)
	if err != nil {
		return nil
	}
	return parsePSI(string(b), kind)
}

func parsePSI(s, kind string) *float64 {
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != kind {
			continue
		}
		for _, kv := range f[1:] {
			if v, ok := strings.CutPrefix(kv, "avg10="); ok {
				if x, err := strconv.ParseFloat(v, 64); err == nil {
					return &x
				}
			}
		}
	}
	return nil
}

var storageFS = map[string]bool{
	"ext4": true, "btrfs": true, "xfs": true, "f2fs": true, "vfat": true,
	"ntfs3": true, "exfat": true, "ext3": true, "zfs": true,
}

func probeStorage(e Env) []Mount {
	b, err := e.FS.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	byDev := map[string]Mount{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !storageFS[f[2]] {
			continue
		}
		mnt := unescapeMount(f[1])
		if strings.HasPrefix(mnt, "/run/") || strings.HasPrefix(mnt, "/var/lib/docker") ||
			strings.HasPrefix(mnt, "/var/lib/containers") || strings.HasPrefix(mnt, "/snap") {
			continue
		}
		// btrfs subvolumes share a device (and free space): keep the shortest mount path.
		if cur, ok := byDev[f[0]]; ok && len(cur.Path) <= len(mnt) {
			continue
		}
		total, avail, err := e.FS.Statfs(mnt)
		if err != nil || total == 0 {
			continue
		}
		byDev[f[0]] = Mount{Path: mnt, Device: f[0], FSType: f[2], TotalBytes: total, FreeBytes: avail}
	}
	out := make([]Mount, 0, len(byDev))
	for _, m := range byDev {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// /proc/mounts escapes space, tab, newline and backslash as octal.
func unescapeMount(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

// ---- GPUs ---------------------------------------------------------------

func probeGPUs(ctx context.Context, e Env) []GPU {
	names := lspciNames(ctx, e)
	ents, err := e.FS.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []GPU
	for _, d := range ents {
		n := d.Name()
		if !strings.HasPrefix(n, "card") || strings.Contains(n, "-") {
			continue
		}
		dev := path.Join("/sys/class/drm", n, "device")
		vendor, ok := platform.ReadTrim(e.FS, dev+"/vendor")
		if !ok {
			continue
		}
		devID, _ := platform.ReadTrim(e.FS, dev+"/device")
		slot := ""
		if t, err := e.FS.Readlink(dev); err == nil {
			slot = path.Base(t)
		}
		if seen[slot+vendor+devID] {
			continue
		}
		seen[slot+vendor+devID] = true
		g := GPU{
			Vendor: vendorName(vendor),
			PCIID:  strings.TrimPrefix(vendor, "0x") + ":" + strings.TrimPrefix(devID, "0x"),
			Slot:   slot,
			Name:   names[slot],
		}
		if t, err := e.FS.Readlink(dev + "/driver"); err == nil {
			g.Driver = path.Base(t)
		}
		if v, ok := platform.ReadTrim(e.FS, dev+"/boot_vga"); ok && v == "1" {
			g.BootVGA = true
		}
		g.RuntimeStatus, _ = platform.ReadTrim(e.FS, dev+"/power/runtime_status")
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

func vendorName(id string) string {
	switch strings.ToLower(id) {
	case "0x10de":
		return "nvidia"
	case "0x1002":
		return "amd"
	case "0x8086":
		return "intel"
	}
	return "other"
}

// lspciNames maps PCI slot ("0000:01:00.0") to a readable device name using
// `lspci -mm -nn` when installed. Absence is fine; names are cosmetic.
func lspciNames(ctx context.Context, e Env) map[string]string {
	out := map[string]string{}
	if !e.Run.Has("lspci") {
		return out
	}
	res, err := e.Run.Run(ctx, "lspci", "-mm", "-nn", "-D")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := splitQuoted(line)
		if len(f) < 4 || !strings.Contains(f[1], "[03") {
			continue
		}
		out[strings.Fields(line)[0]] = cleanPCIName(f[2]) + " " + cleanPCIName(f[3])
	}
	return out
}

// splitQuoted splits `slot "a" "b" "c"` into [slot a b c].
func splitQuoted(line string) []string {
	var out []string
	if i := strings.IndexByte(line, ' '); i > 0 {
		out = append(out, line[:i])
		line = line[i:]
	}
	for {
		a := strings.IndexByte(line, '"')
		if a < 0 {
			return out
		}
		b := strings.IndexByte(line[a+1:], '"')
		if b < 0 {
			return out
		}
		out = append(out, line[a+1:a+1+b])
		line = line[a+b+2:]
	}
}

// cleanPCIName strips the trailing " [10de]" id and the vendor suffix noise.
func cleanPCIName(s string) string {
	if i := strings.LastIndex(s, " ["); i > 0 && strings.HasSuffix(s, "]") && len(s)-i <= 8 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, " Corporation")
	return strings.TrimSpace(s)
}

// ---- tools --------------------------------------------------------------

var toolCatalog = []Tool{
	{Name: "powerprofilesctl", Purpose: "power-profiles-daemon control"},
	{Name: "tuned-adm", Purpose: "TuneD profile management"},
	{Name: "gamemoderun", Purpose: "GameMode launcher"},
	{Name: "mangohud", Purpose: "frame-time logging (benchmarks)"},
	{Name: "nmcli", Purpose: "NetworkManager control"},
	{Name: "busctl", Purpose: "D-Bus (NetworkManager checkpoints)"},
	{Name: "iw", Purpose: "Wi-Fi link details"},
	{Name: "ethtool", Purpose: "Ethernet driver/link details"},
	{Name: "tc", Purpose: "traffic control (bufferbloat/SQM inspection)"},
	{Name: "sensors", Purpose: "temperature sensors"},
	{Name: "nvidia-smi", Purpose: "NVIDIA GPU telemetry"},
	{Name: "lspci", Purpose: "readable GPU names"},
	{Name: "pkexec", Purpose: "polkit authorisation for privileged changes"},
}

func probeTools(e Env) []Tool {
	out := make([]Tool, len(toolCatalog))
	for i, t := range toolCatalog {
		t.Present = e.Run.Has(t.Name)
		out[i] = t
	}
	return out
}

// HasTool reports whether a catalogued tool is present in a report.
func (r Report) HasTool(name string) bool {
	for _, t := range r.Tools {
		if t.Name == name {
			return t.Present
		}
	}
	return false
}
