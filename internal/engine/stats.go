package engine

import (
	"math"
	"sort"
	"sync/atomic"
	"time"
)

// StreamInterval holds the receiver-side measurements of one stream (or the
// sum of all streams) for one report interval.
type StreamInterval struct {
	Stream     int     `json:"stream"` // -1 for the sum
	Bytes      uint64  `json:"bytes"`
	BitsPerSec float64 `json:"bits_per_sec"`
	Packets    uint64  `json:"packets,omitempty"`
	Lost       uint64  `json:"lost,omitempty"`
	LossPct    float64 `json:"loss_pct,omitempty"`
	OutOfOrder uint64  `json:"out_of_order,omitempty"`
	JitterMs   float64 `json:"jitter_ms,omitempty"`
}

// LatencyStats summarises round-trip-time probes.
type LatencyStats struct {
	Sent     int     `json:"sent"`
	Received int     `json:"received"`
	LossPct  float64 `json:"loss_pct"`
	MinMs    float64 `json:"min_ms"`
	AvgMs    float64 `json:"avg_ms"`
	MaxMs    float64 `json:"max_ms"`
	P95Ms    float64 `json:"p95_ms,omitempty"`
	JitterMs float64 `json:"jitter_ms"`
}

// Interval is one row of the per-interval report.
type Interval struct {
	Index   int              `json:"index"`
	Start   float64          `json:"start"` // seconds since test start
	End     float64          `json:"end"`
	Streams []StreamInterval `json:"streams"`
	Sum     StreamInterval   `json:"sum"`
	Latency *LatencyStats    `json:"latency,omitempty"`
}

// Summary is the end-of-test overview.
type Summary struct {
	DurationSec   float64          `json:"duration_sec"`
	Bytes         uint64           `json:"bytes"`
	BitsPerSec    float64          `json:"bits_per_sec"`
	MinBitsPerSec float64          `json:"min_bits_per_sec"`
	MaxBitsPerSec float64          `json:"max_bits_per_sec"`
	SentBytes     uint64           `json:"sent_bytes"`
	Streams       []StreamInterval `json:"streams"`
	// UDP only
	PacketsSent     uint64  `json:"packets_sent,omitempty"`
	PacketsReceived uint64  `json:"packets_received,omitempty"`
	Lost            uint64  `json:"lost,omitempty"`
	LossPct         float64 `json:"loss_pct,omitempty"`
	OutOfOrder      uint64  `json:"out_of_order,omitempty"`
	JitterMs        float64 `json:"jitter_ms,omitempty"`
	// Latency (RTT probes, measured under load)
	Latency *LatencyStats `json:"latency,omitempty"`
}

// Result is the full outcome of one test.
type Result struct {
	Params     TestParams `json:"params"`
	Server     string     `json:"server"`
	ServerHost string     `json:"server_host,omitempty"`
	ClientHost string     `json:"client_host,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	Intervals  []Interval `json:"intervals"`
	Summary    Summary    `json:"summary"`
}

// streamCounter is updated by a single receiver goroutine and sampled by the
// interval ticker.
type streamCounter struct {
	bytes      atomic.Uint64
	packets    atomic.Uint64
	expected   atomic.Uint64 // highest seq + 1 (UDP)
	outOfOrder atomic.Uint64
	jitterBits atomic.Uint64 // float64 ms
}

func (c *streamCounter) setJitter(ms float64) { c.jitterBits.Store(math.Float64bits(ms)) }
func (c *streamCounter) jitter() float64      { return math.Float64frombits(c.jitterBits.Load()) }

type counterSnap struct {
	bytes, packets, expected, ooo uint64
}

// sampler turns cumulative counters into per-interval deltas.
type sampler struct {
	udp      bool
	counters []*streamCounter
	prev     []counterSnap
	start    time.Time
	last     time.Time
	index    int
}

func newSampler(udp bool, counters []*streamCounter) *sampler {
	return &sampler{udp: udp, counters: counters, prev: make([]counterSnap, len(counters))}
}

func (s *sampler) begin(t time.Time) { s.start, s.last = t, t }

func (s *sampler) sample(now time.Time) Interval {
	dt := now.Sub(s.last).Seconds()
	iv := Interval{
		Index: s.index,
		Start: s.last.Sub(s.start).Seconds(),
		End:   now.Sub(s.start).Seconds(),
		Sum:   StreamInterval{Stream: -1},
	}
	s.index++
	s.last = now
	var jitterSum float64
	var jitterN int
	for i, c := range s.counters {
		cur := counterSnap{c.bytes.Load(), c.packets.Load(), c.expected.Load(), c.outOfOrder.Load()}
		p := s.prev[i]
		si := StreamInterval{Stream: i, Bytes: cur.bytes - p.bytes}
		if dt > 0 {
			si.BitsPerSec = float64(si.Bytes) * 8 / dt
		}
		if s.udp {
			si.Packets = cur.packets - p.packets
			exp := cur.expected - p.expected
			if exp > si.Packets {
				si.Lost = exp - si.Packets
			}
			if exp > 0 {
				si.LossPct = float64(si.Lost) * 100 / float64(exp)
			}
			si.OutOfOrder = cur.ooo - p.ooo
			si.JitterMs = c.jitter()
			if si.Packets > 0 {
				jitterSum += si.JitterMs
				jitterN++
			}
		}
		s.prev[i] = cur
		iv.Streams = append(iv.Streams, si)
		iv.Sum.Bytes += si.Bytes
		iv.Sum.BitsPerSec += si.BitsPerSec
		iv.Sum.Packets += si.Packets
		iv.Sum.Lost += si.Lost
		iv.Sum.OutOfOrder += si.OutOfOrder
	}
	if s.udp {
		if exp := iv.Sum.Packets + iv.Sum.Lost; exp > 0 {
			iv.Sum.LossPct = float64(iv.Sum.Lost) * 100 / float64(exp)
		}
		if jitterN > 0 {
			iv.Sum.JitterMs = jitterSum / float64(jitterN)
		}
	}
	return iv
}

// final samples the remaining partial interval. The interval end is capped at
// the planned test end so drain time does not dilute the bitrate. It returns
// false when there is nothing meaningful left to report.
func (s *sampler) final(now, planned time.Time) (Interval, bool) {
	// The last regular tick already reached the planned end: any bytes still
	// arriving are tail data and stay in the totals only.
	if planned.Sub(s.last) < 50*time.Millisecond {
		return Interval{}, false
	}
	t := now
	if now.After(planned) {
		t = planned
	}
	return s.sample(t), true
}

func latencyFrom(sent int, samples []float64) *LatencyStats {
	ls := &LatencyStats{Sent: sent, Received: len(samples)}
	if sent > 0 && sent >= len(samples) {
		ls.LossPct = float64(sent-len(samples)) * 100 / float64(sent)
	}
	if len(samples) == 0 {
		return ls
	}
	ls.MinMs, ls.MaxMs = samples[0], samples[0]
	var sum, jsum float64
	for i, v := range samples {
		sum += v
		if v < ls.MinMs {
			ls.MinMs = v
		}
		if v > ls.MaxMs {
			ls.MaxMs = v
		}
		if i > 0 {
			jsum += math.Abs(v - samples[i-1])
		}
	}
	ls.AvgMs = sum / float64(len(samples))
	if len(samples) > 1 {
		ls.JitterMs = jsum / float64(len(samples)-1)
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	ls.P95Ms = sorted[int(math.Ceil(0.95*float64(len(sorted))))-1]
	return ls
}
