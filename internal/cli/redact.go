package cli

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mttcn83/turboshift/internal/detect"
)

// --redact exists because people paste `turbo doctor --json` into public bug
// reports. The document contains the router's address, the DNS servers and the
// name of every network interface (which can reveal a VPN, Docker or the PCI
// layout of the machine). Redaction is applied to the finished JSON text so it
// also catches addresses that appear inside sentences.
var (
	rePrivateIPv4 = regexp.MustCompile(`\b(10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|169\.254\.\d{1,3}\.\d{1,3}|100\.(?:6[4-9]|[7-9]\d|1[01]\d|12[0-7])\.\d{1,3}\.\d{1,3})\b`)
	rePrivateIPv6 = regexp.MustCompile(`(?i)\b(?:fe80|f[cd][0-9a-f]{2})(?::[0-9a-f]{0,4}){2,7}\b`)
	reMAC         = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}:){5}[0-9a-f]{2}\b`)
)

// interfaceNames lists the names worth hiding (everything except loopback).
func interfaceNames(n detect.Network, extra ...string) []string {
	seen := map[string]bool{"lo": true, "": true}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, i := range n.Interfaces {
		add(i.Name)
	}
	add(n.DefaultIface)
	for _, s := range extra {
		add(s)
	}
	return out
}

// redactJSON replaces private addresses, MAC addresses and the given interface
// names with stable placeholders. The result is still valid JSON because only
// characters inside string values are touched.
func redactJSON(b []byte, ifaces []string) []byte {
	sort.Slice(ifaces, func(i, j int) bool {
		return len(ifaces[i]) > len(ifaces[j]) || len(ifaces[i]) == len(ifaces[j]) && ifaces[i] < ifaces[j]
	})
	for i, name := range ifaces {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		b = re.ReplaceAll(b, []byte(fmt.Sprintf("iface-%d", i+1)))
	}
	b = reMAC.ReplaceAll(b, []byte("mac-redacted"))
	b = rePrivateIPv4.ReplaceAll(b, []byte("private-ip-redacted"))
	b = rePrivateIPv6.ReplaceAll(b, []byte("private-ip6-redacted"))
	return bytes.TrimRight(b, "\n")
}

// jsonRedacted prints v like json() but passes the text through redactJSON.
func (o *out) jsonRedacted(v any, ifaces []string) int {
	var buf bytes.Buffer
	real := o.w
	o.w = &buf
	code := o.json(v)
	o.w = real
	if code != ExitOK {
		return code
	}
	fmt.Fprintln(o.w, strings.TrimSpace(string(redactJSON(buf.Bytes(), ifaces))))
	return ExitOK
}
