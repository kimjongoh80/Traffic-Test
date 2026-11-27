// Package report renders saved test results as a printable HTML report
// (save as PDF from the browser) and as CSV.
package report

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"math"
	"strings"
	"time"

	"github.com/kimjongoh80/Traffic-Test/internal/engine"
)

// Input is everything a report needs.
type Input struct {
	ID       string
	Target   string
	Stopped  bool
	Result   *engine.Result
	Criteria engine.Criteria
	Verdict  engine.Verdict
	Version  string
}

//go:embed report.html.tmpl
var tmplText string

var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"bits":  func(v float64) string { return engine.FormatBits(v) },
	"bytes": func(v uint64) string { return engine.FormatBytes(v) },
	"ms":    func(v float64) string { return fmt.Sprintf("%.2f ms", v) },
	"ms3":   func(v float64) string { return fmt.Sprintf("%.3f ms", v) },
	"pct":   func(v float64) string { return fmtPct(v) },
	"int":   func(v uint64) string { return groupInt(int64(v)) },
	"inti":  func(v int) string { return groupInt(int64(v)) },
	"f1":    func(v float64) string { return fmt.Sprintf("%.1f", v) },
	"f2":    func(v float64) string { return fmt.Sprintf("%.2f", v) },
	"add":   func(a, b int) int { return a + b },
	"sub":   func(a, b int) int { return a - b },
	"lost":  func(s engine.StreamInterval) uint64 { return s.Packets + s.Lost },
	"check": func(n string) string { return checkNames[n] },
	"upper": strings.ToUpper,
}).Parse(tmplText))

var checkNames = map[string]string{"bandwidth": "평균 대역폭", "loss": "패킷 손실률", "rtt": "평균 RTT", "jitter": "지터"}

func fmtPct(v float64) string {
	switch {
	case v == 0:
		return "0%"
	case v < 0.01:
		return fmt.Sprintf("%.4f%%", v)
	case v < 1:
		return fmt.Sprintf("%.3f%%", v)
	}
	return fmt.Sprintf("%.2f%%", v)
}

func groupInt(n int64) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func axisBits(v float64) string {
	switch {
	case v == 0:
		return "0"
	case v >= 1e9:
		return trim(fmt.Sprintf("%.2f", v/1e9)) + "G"
	case v >= 1e6:
		return trim(fmt.Sprintf("%.1f", v/1e6)) + "M"
	case v >= 1e3:
		return trim(fmt.Sprintf("%.1f", v/1e3)) + "K"
	}
	return fmt.Sprintf("%.0f", v)
}

func trim(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

type kv struct{ K, V string }

type streamRow struct {
	Name               string
	Bytes              uint64
	Bps                float64
	Packets, Lost, OOO uint64
	LossPct, Jitter    float64
	Sum                bool
}

type view struct {
	In        Input
	R         *engine.Result
	S         engine.Summary
	UDP       bool
	Title     string
	Generated string
	Started   string
	Sender    string
	Receiver  string
	Mode      string
	Info      []kv
	Summary   []kv
	Charts    []template.HTML
	Streams   []streamRow
	Intervals []engine.Interval
}

// HTML writes a self-contained report page. Use the browser's print dialog
// ("Save as PDF") to produce a PDF.
func HTML(w io.Writer, in Input) error {
	r := in.Result
	if r == nil {
		return fmt.Errorf("no result")
	}
	s := r.Summary
	p := r.Params
	udp := p.Protocol == "udp"
	v := view{In: in, R: r, S: s, UDP: udp,
		Title:     "트래픽 스트레스 테스트 보고서",
		Generated: time.Now().Format("2006-01-02 15:04:05"),
		Started:   r.StartedAt.Local().Format("2006-01-02 15:04:05"),
		Intervals: r.Intervals,
	}
	client, server := nz(r.ClientHost, "시험 PC"), nz(r.ServerHost, "상대 PC")
	if p.Reverse {
		v.Sender, v.Receiver, v.Mode = server, client, "Client (시험 PC가 수신)"
	} else {
		v.Sender, v.Receiver, v.Mode = client, server, "Server (시험 PC가 송신)"
	}
	target := "최대"
	if p.Bandwidth > 0 {
		target = engine.FormatBits(float64(p.Bandwidth))
	}
	lenName := "버퍼 크기"
	if udp {
		lenName = "패킷 크기"
	}
	v.Info = []kv{
		{"송신 PC → 수신 PC", v.Sender + " → " + v.Receiver},
		{"상대 PC 주소", in.Target},
		{"Mode", v.Mode},
		{"프로토콜", strings.ToUpper(p.Protocol)},
		{"병렬 스트림", fmt.Sprint(p.Streams)},
		{"설정 시간", fmt.Sprintf("%g 초", p.DurationSec)},
		{"목표 대역폭", target},
		{lenName, groupInt(int64(p.Length)) + " 바이트"},
		{"RTT 측정 간격", rttInterval(p.LatencyMs)},
		{"시작 시각", v.Started},
	}
	v.Summary = []kv{
		{"평균 대역폭", engine.FormatBits(s.BitsPerSec)},
		{"최소 / 최대", engine.FormatBits(s.MinBitsPerSec) + " / " + engine.FormatBits(s.MaxBitsPerSec)},
		{"수신량 (송신량)", engine.FormatBytes(s.Bytes) + " (" + engine.FormatBytes(s.SentBytes) + ")"},
		{"측정 시간", fmt.Sprintf("%.2f 초", s.DurationSec)},
	}
	if udp {
		v.Summary = append(v.Summary,
			kv{"패킷 손실", fmt.Sprintf("%s (%s / %s)", fmtPct(s.LossPct), groupInt(int64(s.Lost)), groupInt(int64(s.PacketsSent)))},
			kv{"UDP 지터", fmt.Sprintf("%.3f ms", s.JitterMs)},
			kv{"순서 뒤바뀜", groupInt(int64(s.OutOfOrder))})
	}
	if l := s.Latency; l != nil && l.Received > 0 {
		v.Summary = append(v.Summary,
			kv{"RTT 평균 / p95", fmt.Sprintf("%.2f ms / %.2f ms", l.AvgMs, l.P95Ms)},
			kv{"RTT 최소 / 최대", fmt.Sprintf("%.2f ms / %.2f ms", l.MinMs, l.MaxMs)},
			kv{"RTT 지터", fmt.Sprintf("%.2f ms", l.JitterMs)},
			kv{"RTT 프로브 손실", fmt.Sprintf("%s (%d / %d)", fmtPct(l.LossPct), l.Sent-l.Received, l.Sent)})
	}
	v.Charts = buildCharts(r, in.Criteria)
	v.Streams = buildStreams(r)
	return tmpl.Execute(w, v)
}

func rttInterval(ms int) string {
	if ms <= 0 {
		return "측정 안 함"
	}
	return fmt.Sprintf("%d ms", ms)
}

func nz(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func buildCharts(r *engine.Result, c engine.Criteria) []template.HTML {
	udp := r.Params.Protocol == "udp"
	n := len(r.Intervals)
	xs := make([]float64, n)
	bw, ravg, rmax, rj, uj, loss := mk(n), mk(n), mk(n), mk(n), mk(n), mk(n)
	for i, iv := range r.Intervals {
		xs[i] = iv.End
		bw[i] = iv.Sum.BitsPerSec
		if l := iv.Latency; l != nil && l.Received > 0 {
			ravg[i], rmax[i] = l.AvgMs, l.MaxMs
			if l.Received > 1 {
				rj[i] = l.JitterMs
			}
		}
		if udp {
			uj[i], loss[i] = iv.Sum.JitterMs, iv.Sum.LossPct
		} else if l := iv.Latency; l != nil {
			loss[i] = l.LossPct
		}
	}
	const c1, c2, c3, c4 = "#2563eb", "#db2777", "#059669", "#d97706"
	msFmt := func(v float64) string { return trim(fmt.Sprintf("%.3f", v)) }
	out := []template.HTML{}
	bwSpec := chartSpec{Title: "대역폭", Xs: xs, XMax: r.Params.DurationSec, YFmt: axisBits,
		Series: []series{{Name: "대역폭 (bps)", Color: c1, Ys: bw}}}
	if c.MinBitsPerSec > 0 {
		bwSpec.Threshold, bwSpec.ThLabel = c.MinBitsPerSec, "최소 "+engine.FormatBits(c.MinBitsPerSec)
	}
	out = append(out, renderChart(bwSpec))
	rttSpec := chartSpec{Title: "지연시간", Xs: xs, XMax: r.Params.DurationSec, YFmt: msFmt,
		Series: []series{{Name: "RTT 평균 (ms)", Color: c1, Ys: ravg}, {Name: "RTT 최대 (ms)", Color: c2, Ys: rmax}}}
	if c.MaxRTTMs > 0 {
		rttSpec.Threshold, rttSpec.ThLabel = c.MaxRTTMs, fmt.Sprintf("최대 %.2f ms", c.MaxRTTMs)
	}
	out = append(out, renderChart(rttSpec))
	jitSeries := []series{{Name: "RTT 지터 (ms)", Color: c3, Ys: rj}}
	if udp {
		jitSeries = []series{{Name: "UDP 지터 (ms)", Color: c3, Ys: uj}, {Name: "RTT 지터 (ms)", Color: c2, Ys: rj, Dash: true}}
	}
	jitSpec := chartSpec{Title: "지터", Xs: xs, XMax: r.Params.DurationSec, YFmt: msFmt, Series: jitSeries}
	if c.MaxJitterMs > 0 {
		jitSpec.Threshold, jitSpec.ThLabel = c.MaxJitterMs, fmt.Sprintf("최대 %.3f ms", c.MaxJitterMs)
	}
	out = append(out, renderChart(jitSpec))
	lossName := "RTT 프로브 손실률 (%)"
	if udp {
		lossName = "패킷 손실률 (%)"
	}
	lossSpec := chartSpec{Title: "손실", Xs: xs, XMax: r.Params.DurationSec,
		YFmt:   func(v float64) string { return trim(fmt.Sprintf("%.3f", v)) + "%" },
		Series: []series{{Name: lossName, Color: c4, Ys: loss}}}
	if udp && c.CheckLoss && c.MaxLossPct > 0 {
		lossSpec.Threshold, lossSpec.ThLabel = c.MaxLossPct, "최대 "+fmtPct(c.MaxLossPct)
	}
	out = append(out, renderChart(lossSpec))
	return out
}

func mk(n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = math.NaN()
	}
	return s
}

func buildStreams(r *engine.Result) []streamRow {
	s := r.Summary
	var rows []streamRow
	var tot streamRow
	tot.Name, tot.Sum = "합계", true
	for i, st := range s.Streams {
		row := streamRow{Name: fmt.Sprintf("#%d", i+1), Bytes: st.Bytes, Bps: st.BitsPerSec,
			Packets: st.Packets, Lost: st.Lost, LossPct: st.LossPct, OOO: st.OutOfOrder, Jitter: st.JitterMs}
		rows = append(rows, row)
		tot.Bytes += st.Bytes
		tot.Bps += st.BitsPerSec
		tot.Packets += st.Packets
		tot.Lost += st.Lost
		tot.OOO += st.OutOfOrder
	}
	if len(rows) > 1 {
		tot.LossPct = s.LossPct
		tot.Jitter = s.JitterMs
		rows = append(rows, tot)
	}
	return rows
}
