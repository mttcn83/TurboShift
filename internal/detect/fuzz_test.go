package detect

import "testing"

// The parsers read files that other software controls; none may panic.
func FuzzParsers(f *testing.F) {
	f.Add("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\nwlan0\t00000000\t0101A8C0\t0003\t0\t0\t600\n")
	f.Add("some avg10=0.50 avg60=1 total=3\nfull avg10=x\n")
	f.Add("MemTotal: 1 kB\nSwapFree:\n")
	f.Add("nameserver 1.1.1.1\nsearch a b\n")
	f.Add("00:02.0 \"VGA\" \"Intel [8086]\" \"x\"")
	f.Add("a\\040b\\")
	f.Fuzz(func(t *testing.T, s string) {
		ParseDefaultRoute(s)
		parsePSI(s, "some")
		parseMeminfo(s)
		ParseResolvConf(s)
		ParseIWPowerSave(s)
		ParsePPDProfiles(s)
		parseKV(s)
		splitQuoted(s)
		cleanPCIName(s)
		unescapeMount(s)
		FormatBytes(uint64(len(s)) << 40)
	})
}
