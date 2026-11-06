package benchmark

import (
	"strings"
	"testing"
)

// FuzzParseAndSummarize feeds arbitrary bytes through the log parser and the
// summariser. Neither may panic, and a successful parse must satisfy the
// invariants the rest of the code indexes by.
func FuzzParseAndSummarize(f *testing.F) {
	f.Add("os,cpu,gpu,ram,kernel,driver,cpuscheduler\nLinux,x,y,1,2,3,4\nfps,frametime,cpu_load,gpu_load,cpu_temp,gpu_temp,gpu_power,elapsed\n60,16.6,1,2,3,4,5,1000000\n")
	f.Add("fps,frametime,elapsed\n1,1,1\n1,0,2\n,,\n")
	f.Add("---\nfps,frametime\n" + strings.Repeat("60,16.6\n", 150))
	f.Fuzz(func(t *testing.T, in string) {
		fr, err := ParseMangoHud(strings.NewReader(in))
		if err != nil {
			return
		}
		if len(fr.FPS) == 0 || len(fr.FPS) != len(fr.FrameMs) || len(fr.FPS) != len(fr.ElapsedS) {
			t.Fatalf("inconsistent frames: %d %d %d", len(fr.FPS), len(fr.FrameMs), len(fr.ElapsedS))
		}
		r, err := Summarize(fr, ImportOptions{})
		if err != nil {
			return
		}
		if r.AvgFPS != r.AvgFPS || r.Low1FPS != r.Low1FPS { // NaN
			t.Fatalf("NaN in summary: %+v", r)
		}
	})
}
