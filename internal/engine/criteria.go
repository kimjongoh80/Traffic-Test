package engine

import "fmt"

// Criteria are optional pass/fail limits. Zero values are not evaluated.
type Criteria struct {
	MinBitsPerSec float64 `json:"min_bits_per_sec,omitempty"` // average bandwidth must be >= this
	MaxLossPct    float64 `json:"max_loss_pct,omitempty"`     // UDP packet loss must be <= this
	MaxRTTMs      float64 `json:"max_rtt_ms,omitempty"`       // average RTT must be <= this
	MaxJitterMs   float64 `json:"max_jitter_ms,omitempty"`    // UDP jitter (or RTT jitter for TCP) must be <= this
	// CheckLoss enables the loss check even when MaxLossPct is 0 (zero loss required).
	CheckLoss bool `json:"check_loss,omitempty"`
}

// Check is the outcome of one criterion.
type Check struct {
	Name     string  `json:"name"`
	Limit    string  `json:"limit"`
	Measured string  `json:"measured"`
	Value    float64 `json:"value"`
	Pass     bool    `json:"pass"`
}

// Verdict is the overall pass/fail evaluation of a result.
type Verdict struct {
	Evaluated bool    `json:"evaluated"`
	Pass      bool    `json:"pass"`
	Checks    []Check `json:"checks"`
}

// Evaluate applies the criteria to a result.
func Evaluate(r *Result, c Criteria) Verdict {
	v := Verdict{Pass: true}
	add := func(ch Check) {
		v.Checks = append(v.Checks, ch)
		v.Evaluated = true
		if !ch.Pass {
			v.Pass = false
		}
	}
	s := r.Summary
	if c.MinBitsPerSec > 0 {
		add(Check{Name: "bandwidth", Limit: ">= " + FormatBits(c.MinBitsPerSec),
			Measured: FormatBits(s.BitsPerSec), Value: s.BitsPerSec, Pass: s.BitsPerSec >= c.MinBitsPerSec})
	}
	if r.Params.Protocol == "udp" && (c.MaxLossPct > 0 || c.CheckLoss) {
		add(Check{Name: "loss", Limit: fmt.Sprintf("<= %.3f%%", c.MaxLossPct),
			Measured: fmt.Sprintf("%.3f%%", s.LossPct), Value: s.LossPct, Pass: s.LossPct <= c.MaxLossPct})
	}
	if c.MaxRTTMs > 0 {
		ch := Check{Name: "rtt", Limit: fmt.Sprintf("<= %.2f ms", c.MaxRTTMs), Measured: "no data"}
		if s.Latency != nil && s.Latency.Received > 0 {
			ch.Value = s.Latency.AvgMs
			ch.Measured = fmt.Sprintf("%.2f ms", s.Latency.AvgMs)
			ch.Pass = s.Latency.AvgMs <= c.MaxRTTMs
		}
		add(ch)
	}
	if c.MaxJitterMs > 0 {
		j, ok := s.JitterMs, r.Params.Protocol == "udp"
		if !ok && s.Latency != nil && s.Latency.Received > 1 {
			j, ok = s.Latency.JitterMs, true
		}
		ch := Check{Name: "jitter", Limit: fmt.Sprintf("<= %.3f ms", c.MaxJitterMs), Measured: "no data"}
		if ok {
			ch.Value, ch.Measured, ch.Pass = j, fmt.Sprintf("%.3f ms", j), j <= c.MaxJitterMs
		}
		add(ch)
	}
	if !v.Evaluated {
		v.Pass = false
	}
	return v
}
