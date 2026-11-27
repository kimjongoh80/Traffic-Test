package report

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/kimjongoh80/Traffic-Test/internal/engine"
)

// CSV writes one row per interval and stream (stream "SUM" for the total).
// A UTF-8 BOM is written first so Excel shows Korean headers correctly.
func CSV(w io.Writer, r *engine.Result) error {
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"구간 시작(s)", "구간 끝(s)", "스트림", "전송량(bytes)", "대역폭(bps)", "대역폭(Mbps)",
		"수신 패킷", "손실 패킷", "손실률(%)", "순서 뒤바뀜", "UDP 지터(ms)",
		"RTT 평균(ms)", "RTT 최소(ms)", "RTT 최대(ms)", "RTT 지터(ms)", "프로브 송신", "프로브 수신",
	})
	f := func(v float64, d int) string { return fmt.Sprintf("%.*f", d, v) }
	for _, iv := range r.Intervals {
		lat := []string{"", "", "", "", "", ""}
		if l := iv.Latency; l != nil {
			lat = []string{"", "", "", "", fmt.Sprint(l.Sent), fmt.Sprint(l.Received)}
			if l.Received > 0 {
				lat[0], lat[1], lat[2], lat[3] = f(l.AvgMs, 3), f(l.MinMs, 3), f(l.MaxMs, 3), f(l.JitterMs, 3)
			}
		}
		rows := append([]engine.StreamInterval{}, iv.Streams...)
		if len(iv.Streams) > 1 {
			rows = append(rows, iv.Sum)
		}
		for _, s := range rows {
			name := fmt.Sprint(s.Stream + 1)
			l := []string{"", "", "", "", "", ""}
			if s.Stream < 0 || len(iv.Streams) == 1 {
				if s.Stream < 0 {
					name = "SUM"
				}
				l = lat
			}
			_ = cw.Write(append([]string{
				f(iv.Start, 2), f(iv.End, 2), name, fmt.Sprint(s.Bytes), f(s.BitsPerSec, 0), f(s.BitsPerSec/1e6, 3),
				fmt.Sprint(s.Packets), fmt.Sprint(s.Lost), f(s.LossPct, 4), fmt.Sprint(s.OutOfOrder), f(s.JitterMs, 4),
			}, l...))
		}
	}
	cw.Flush()
	return cw.Error()
}
