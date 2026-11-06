package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/diagnostics"
)

func cmdNetwork(ctx context.Context, a *app.App, args []string, o *out) int {
	if len(args) > 0 && args[0] == "doctor" {
		args = args[1:]
	}
	fs := newFlags("network", o)
	ext := fs.Bool("external", false, "allow tests that contact hosts beyond your router (you consent to the destinations listed)")
	target := fs.String("target", a.Prefs.PingTarget, "host to ping for the internet test")
	lookup := fs.String("lookup", a.Prefs.LookupName, "name to resolve for the DNS test")
	down := fs.String("load-url", "", "http(s) URL of a large file to download while measuring latency (bufferbloat test)")
	up := fs.String("upload-url", "", "http(s) URL that accepts a POST body, for the upload bufferbloat test")
	secs := fs.Int("load-seconds", 10, "duration of each load test")
	asJSON := fs.Bool("json", false, "machine-readable output")
	ssid := fs.Bool("include-ssid", false, "include your Wi-Fi network name in --json output")
	if _, err := parse(fs, args); err != nil {
		return ExitUsage
	}
	if *secs < 1 || *secs > diagnostics.MaxLoadSeconds {
		return usageErr(o, fmt.Sprintf("--load-seconds must be between 1 and %d", diagnostics.MaxLoadSeconds))
	}
	if (*down != "" || *up != "") && !*ext {
		return usageErr(o, "--load-url/--upload-url contact external hosts; add --external to confirm")
	}
	opt := diagnostics.NetOptions{External: *ext, PingTarget: *target, LookupName: *lookup, LoadDownURL: *down, LoadUpURL: *up, LoadSeconds: *secs}

	rep := a.Detect(ctx)
	dests := diagnostics.PlanDestinations(rep, opt)
	if !*asJSON {
		o.heading("network doctor")
		o.println("  This run will contact:")
		for _, d := range dests {
			tag := "local   "
			if d.External {
				tag = "external"
			}
			o.printf("    %s  %-24s %s\n", o.dim(tag), d.Host, o.dim(d.Purpose))
		}
		if !*ext {
			o.printf("    %s\n", o.dim("(add --external to also test DNS and internet reachability)"))
		}
		o.println()
	}
	var progress func(string)
	if !*asJSON {
		progress = func(s string) { o.printf("  %s %s…\n", o.dim("·"), s) }
	}
	res := diagnostics.CollectNetwork(ctx, rep, opt, diagnostics.RealNetDeps(a.FS, a.Run), progress)

	if *asJSON {
		if !*ssid {
			res.Link.SSID = ""
		}
		return o.json(res)
	}
	o.println()
	l := res.Link
	switch l.Kind {
	case "wifi":
		sig := "signal unknown"
		if l.SignalDBm != nil {
			sig = fmt.Sprintf("%d dBm", *l.SignalDBm)
		}
		o.printf("  %-9s Wi-Fi %s · %s", o.dim("Link"), l.Iface, sig)
		if l.FreqMHz != nil {
			o.printf(" · %.1f GHz", *l.FreqMHz/1000)
		}
		if l.BitrateMbps != nil {
			o.printf(" · %.0f Mbit/s", *l.BitrateMbps)
		}
		if l.PowerSave != "" {
			o.printf(" · power save %s", l.PowerSave)
		}
		o.println()
	case "ethernet":
		o.printf("  %-9s Ethernet %s · %s\n", o.dim("Link"), l.Iface, speed(l.SpeedMbps))
	default:
		o.printf("  %-9s no active network link\n", o.dim("Link"))
	}
	pr := func(name string, s *diagnostics.PingStats) {
		if s != nil {
			o.printf("  %-9s %-16s loss %4.1f%% · p50 %5.1f ms · p95 %5.1f ms · jitter %4.1f ms\n", o.dim(name), s.Target, s.LossPct, s.P50Ms, s.P95Ms, s.JitterMs)
		}
	}
	pr("Router", res.Gateway)
	for _, d := range res.DNS {
		o.printf("  %-9s %-16s first %5.0f ms · median %5.0f ms · failures %d/5\n", o.dim("DNS"), d.Server, d.FirstMs, d.MedianMs, d.Failures)
	}
	pr("Internet", res.Internet)
	for _, ls := range []*diagnostics.LoadStats{res.Download, res.Upload} {
		if ls != nil && ls.Error == "" {
			o.printf("  %-9s %-16s %.0f Mbit/s · p50 %5.1f ms · p95 %5.1f ms under load\n", o.dim(strings.ToUpper(ls.Direction[:1])+ls.Direction[1:]), ls.Host, ls.Mbps, ls.Under.P50Ms, ls.Under.P95Ms)
		}
	}

	o.printf("\n  %s\n", o.bold("Diagnosis"))
	for _, d := range res.Diagnosis {
		o.printf("  %s %-8s %s %s\n", o.sev(d.Sev), d.Layer, d.Title, o.dim("("+d.Certainty+" certainty)"))
		if d.Sev >= diagnostics.SevNotice && d.Detail != "" {
			o.printf("%s\n", o.dim("            "+wrap(d.Detail, 72, "            ")))
		}
	}
	for _, s := range res.Skipped {
		o.printf("  %s %s\n", o.dim("SKIP  "), o.dim(s))
	}
	o.printf("\n  %s\n\n", o.dim("ICMP can be rate-limited or deprioritised by some routers; treat single short runs as indicative, not proof."))
	return ExitOK
}

func speed(s *int) string {
	if s == nil {
		return "speed unknown"
	}
	return fmt.Sprintf("%d Mbit/s", *s)
}
