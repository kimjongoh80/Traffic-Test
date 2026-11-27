package engine

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestParseBits(t *testing.T) {
	cases := map[string]uint64{"": 0, "1G": 1e9, "25g": 25e9, "500M": 500e6, "100k": 100e3, "1.5Gbps": 1.5e9, "1000": 1000}
	for in, want := range cases {
		got, err := ParseBits(in)
		if err != nil || got != want {
			t.Errorf("ParseBits(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := ParseBits("abc"); err == nil {
		t.Error("expected error for abc")
	}
}

func TestEvaluate(t *testing.T) {
	r := &Result{Params: TestParams{Protocol: "udp"}, Summary: Summary{
		BitsPerSec: 900e6, LossPct: 0.5, JitterMs: 0.2,
		Latency: &LatencyStats{Received: 10, AvgMs: 1.5},
	}}
	v := Evaluate(r, Criteria{MinBitsPerSec: 800e6, MaxLossPct: 0.1, MaxRTTMs: 2, MaxJitterMs: 1})
	if !v.Evaluated || v.Pass {
		t.Fatalf("want evaluated fail, got %+v", v)
	}
	for _, c := range v.Checks {
		if c.Name == "loss" && c.Pass {
			t.Error("loss should fail")
		}
		if c.Name != "loss" && !c.Pass {
			t.Errorf("%s should pass", c.Name)
		}
	}
	if v := Evaluate(r, Criteria{}); v.Evaluated {
		t.Error("empty criteria should not be evaluated")
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startServer(t *testing.T) int {
	port := freePort(t)
	srv := &Server{Port: port, Bind: "127.0.0.1"}
	go srv.ListenAndServe()
	t.Cleanup(func() { srv.Close() })
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", itoa(port))); err == nil {
			c.Close()
			return port
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not start")
	return 0
}

func TestLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("loopback test skipped in -short mode")
	}
	port := startServer(t)
	cases := []struct {
		name string
		p    TestParams
	}{
		{"tcp", TestParams{Protocol: "tcp", Streams: 2, Bandwidth: 200e6}},
		{"tcp-reverse", TestParams{Protocol: "tcp", Reverse: true, Bandwidth: 200e6}},
		{"udp", TestParams{Protocol: "udp", Streams: 2, Bandwidth: 50e6}},
		{"udp-reverse", TestParams{Protocol: "udp", Reverse: true, Bandwidth: 50e6}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.p
			p.DurationSec, p.IntervalSec, p.LatencyMs = 2, 0.5, 50
			var n int
			res, err := RunClient(context.Background(), "127.0.0.1", port, p, func(Interval) { n++ })
			if err != nil {
				t.Fatal(err)
			}
			s := res.Summary
			if n < 3 || len(res.Intervals) != n {
				t.Errorf("got %d intervals", n)
			}
			// Rate pacing should land within 20% of the target.
			want := float64(p.Bandwidth)
			if s.BitsPerSec < want*0.8 || s.BitsPerSec > want*1.2 {
				t.Errorf("bandwidth %s, want about %s", FormatBits(s.BitsPerSec), FormatBits(want))
			}
			if len(s.Streams) != p.Streams && !(p.Streams == 0 && len(s.Streams) == 1) {
				t.Errorf("got %d stream summaries", len(s.Streams))
			}
			if s.Latency == nil || s.Latency.Received == 0 {
				t.Error("no latency samples")
			}
			if p.Protocol == "udp" {
				if s.PacketsSent == 0 || s.PacketsReceived == 0 {
					t.Errorf("packets sent %d received %d", s.PacketsSent, s.PacketsReceived)
				}
				if s.LossPct > 5 {
					t.Errorf("loss %.2f%% on loopback", s.LossPct)
				}
			}
		})
	}
}

func TestCancel(t *testing.T) {
	port := startServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := RunClient(ctx, "127.0.0.1", port, TestParams{Protocol: "tcp", DurationSec: 30, Bandwidth: 100e6}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("cancel took %v", time.Since(start))
	}
	if res.Summary.Bytes == 0 {
		t.Error("expected partial results")
	}
}

func TestBusyServer(t *testing.T) {
	port := startServer(t)
	done := make(chan struct{})
	go func() {
		RunClient(context.Background(), "127.0.0.1", port, TestParams{Protocol: "tcp", DurationSec: 1.5, Bandwidth: 10e6}, nil)
		close(done)
	}()
	time.Sleep(300 * time.Millisecond)
	_, err := RunClient(context.Background(), "127.0.0.1", port, TestParams{Protocol: "tcp", DurationSec: 1}, nil)
	if err == nil {
		t.Error("second test on a busy server should fail")
	}
	<-done
}
