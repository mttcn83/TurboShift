package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/mttcn83/turboshift/internal/stats"
)

// PingStats summarises one ICMP run. RTTs are in milliseconds.
type PingStats struct {
	Target   string    `json:"target"`
	Sent     int       `json:"sent"`
	Received int       `json:"received"`
	LossPct  float64   `json:"loss_pct"`
	MinMs    float64   `json:"min_ms"`
	AvgMs    float64   `json:"avg_ms"`
	P50Ms    float64   `json:"p50_ms"`
	P95Ms    float64   `json:"p95_ms"`
	MaxMs    float64   `json:"max_ms"`
	JitterMs float64   `json:"jitter_ms"`
	RTTs     []float64 `json:"-"`
}

// Summarize builds PingStats from the RTTs (ms, in arrival order) of `sent` probes.
func Summarize(target string, sent int, rtts []float64) PingStats {
	s := PingStats{Target: target, Sent: sent, Received: len(rtts), RTTs: rtts}
	if sent > 0 {
		s.LossPct = float64(sent-len(rtts)) / float64(sent) * 100
	}
	if len(rtts) > 0 {
		s.MinMs, s.MaxMs, s.AvgMs = stats.Min(rtts), stats.Max(rtts), stats.Mean(rtts)
		s.P50Ms, s.P95Ms = stats.Median(rtts), stats.Percentile(rtts, 95)
		if j := stats.MeanAbsDiff(rtts); j == j { // not NaN
			s.JitterMs = j
		}
	}
	return s
}

// Pinger sends `count` echo requests `interval` apart.
type Pinger interface {
	Ping(ctx context.Context, host string, count int, interval time.Duration) (PingStats, error)
}

// ICMPPinger uses unprivileged ICMP datagram sockets (no root, no `ping`
// binary). It needs the user's group inside net.ipv4.ping_group_range, which
// systemd-based distributions set to all groups by default.
type ICMPPinger struct{ Timeout time.Duration }

var ErrNoICMP = errors.New("cannot open an unprivileged ICMP socket (check net.ipv4.ping_group_range)")

func (p ICMPPinger) Ping(ctx context.Context, host string, count int, interval time.Duration) (PingStats, error) {
	ip, err := resolveHost(ctx, host)
	if err != nil {
		return PingStats{Target: host}, err
	}
	network, proto, reqType := "udp4", 1, icmp.Type(ipv4.ICMPTypeEcho)
	var replyOK = func(t icmp.Type) bool { return t == ipv4.ICMPTypeEchoReply }
	listen := "0.0.0.0"
	if ip.To4() == nil {
		network, proto, reqType, listen = "udp6", 58, ipv6.ICMPTypeEchoRequest, "::"
		replyOK = func(t icmp.Type) bool { return t == ipv6.ICMPTypeEchoReply }
	}
	conn, err := icmp.ListenPacket(network, listen)
	if err != nil {
		return PingStats{Target: host}, fmt.Errorf("%w: %v", ErrNoICMP, err)
	}
	defer conn.Close()

	timeout := p.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	var mu sync.Mutex
	sentAt := map[int]time.Time{}
	rtt := map[int]float64{}
	done := make(chan struct{})
	go func() { // reader
		defer close(done)
		buf := make([]byte, 1500)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _, err := conn.ReadFrom(buf)
			now := time.Now()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					mu.Lock()
					finished := len(sentAt) == count && now.Sub(lastSent(sentAt)) > timeout
					mu.Unlock()
					if finished {
						return
					}
					continue
				}
				return
			}
			m, err := icmp.ParseMessage(proto, buf[:n])
			if err != nil || !replyOK(m.Type) {
				continue
			}
			if e, ok := m.Body.(*icmp.Echo); ok {
				mu.Lock()
				if t0, ok := sentAt[e.Seq]; ok {
					if _, dup := rtt[e.Seq]; !dup {
						rtt[e.Seq] = float64(now.Sub(t0).Microseconds()) / 1000
					}
				}
				mu.Unlock()
			}
		}
	}()

	dst := &net.UDPAddr{IP: ip}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for i := 0; i < count; i++ {
		msg := icmp.Message{Type: reqType, Code: 0, Body: &icmp.Echo{ID: 0, Seq: i, Data: []byte("turboshift")}}
		b, _ := msg.Marshal(nil)
		mu.Lock()
		sentAt[i] = time.Now()
		mu.Unlock()
		_, _ = conn.WriteTo(b, dst) // send errors surface as loss
		if i < count-1 {
			select {
			case <-ctx.Done():
				conn.Close()
				<-done
				return collect(host, count, rtt, &mu), ctx.Err()
			case <-tick.C:
			}
		}
	}
	<-done
	return collect(host, count, rtt, &mu), nil
}

func lastSent(m map[int]time.Time) time.Time {
	var t time.Time
	for _, v := range m {
		if v.After(t) {
			t = v
		}
	}
	return t
}

func collect(host string, sent int, rtt map[int]float64, mu *sync.Mutex) PingStats {
	mu.Lock()
	defer mu.Unlock()
	seqs := make([]int, 0, len(rtt))
	for s := range rtt {
		seqs = append(seqs, s)
	}
	sort.Ints(seqs)
	rs := make([]float64, len(seqs))
	for i, s := range seqs {
		rs[i] = rtt[s]
	}
	return Summarize(host, sent, rs)
}

func resolveHost(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("cannot resolve %q: %v", host, err)
	}
	for _, a := range ips {
		if a.IP.To4() != nil {
			return a.IP, nil
		}
	}
	return ips[0].IP, nil
}
