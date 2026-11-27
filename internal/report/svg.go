package report

import (
	"fmt"
	"html/template"
	"math"
	"strings"
)

type series struct {
	Name  string
	Color string
	Dash  bool
	Ys    []float64 // NaN = missing
}

type chartSpec struct {
	Title     string
	Xs        []float64
	XMax      float64
	Series    []series
	YFmt      func(float64) string
	Threshold float64
	ThLabel   string
}

func niceStep(r float64, ticks int) float64 {
	if r <= 0 {
		return 1
	}
	raw := r / float64(ticks)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	n := raw / mag
	switch {
	case n < 1.5:
		n = 1
	case n < 3:
		n = 2
	case n < 7:
		n = 5
	default:
		n = 10
	}
	return n * mag
}

// renderChart draws a line chart as inline SVG (prints crisply, no scripts).
func renderChart(c chartSpec) template.HTML {
	const W, H = 760.0, 230.0
	const L, R, T, B = 64.0, 12.0, 26.0, 26.0
	pw, ph := W-L-R, H-T-B
	maxY := 0.0
	for _, s := range c.Series {
		for _, v := range s.Ys {
			if !math.IsNaN(v) && v > maxY {
				maxY = v
			}
		}
	}
	if c.Threshold > 0 && c.Threshold*1.1 > maxY {
		maxY = c.Threshold * 1.1
	}
	if maxY <= 0 {
		maxY = 1
	}
	step := niceStep(maxY, 4)
	maxY = math.Ceil(maxY/step) * step
	xmax := c.XMax
	for _, x := range c.Xs {
		if x > xmax {
			xmax = x
		}
	}
	if xmax <= 0 {
		xmax = 1
	}
	X := func(x float64) float64 { return L + x/xmax*pw }
	Y := func(y float64) float64 { return T + ph - y/maxY*ph }

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart" viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="%s">`, W, H, template.HTMLEscapeString(c.Title))
	for v := 0.0; v <= maxY+step/1e6; v += step {
		y := Y(v)
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" class="grid"/>`, L, y, W-R, y)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" class="ax" text-anchor="end" dominant-baseline="middle">%s</text>`, L-6, y, template.HTMLEscapeString(c.YFmt(v)))
	}
	xs := math.Max(1, niceStep(xmax, 8))
	for x := 0.0; x <= xmax+1e-9; x += xs {
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" class="ax" text-anchor="middle">%.0fs</text>`, X(x), T+ph+17, x)
	}
	if c.Threshold > 0 {
		y := Y(c.Threshold)
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" class="th"/>`, L, y, W-R, y)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" class="thl" text-anchor="end">%s</text>`, W-R-2, y-4, template.HTMLEscapeString(c.ThLabel))
	}
	for _, s := range c.Series {
		var pts []string
		flush := func() {
			if len(pts) > 0 {
				dash := ""
				if s.Dash {
					dash = ` stroke-dasharray="5 4"`
				}
				fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="2"%s points="%s"/>`, s.Color, dash, strings.Join(pts, " "))
				if len(pts) == 1 {
					var px, py float64
					fmt.Sscanf(pts[0], "%f,%f", &px, &py)
					fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="2.5" fill="%s"/>`, px, py, s.Color)
				}
			}
			pts = pts[:0]
		}
		for i, v := range s.Ys {
			if i >= len(c.Xs) || math.IsNaN(v) {
				flush()
				continue
			}
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", X(c.Xs[i]), Y(v)))
		}
		flush()
	}
	lx := L
	for _, s := range c.Series {
		fmt.Fprintf(&b, `<rect x="%.1f" y="8" width="12" height="3" fill="%s"/>`, lx, s.Color)
		fmt.Fprintf(&b, `<text x="%.1f" y="12" class="lg">%s</text>`, lx+16, template.HTMLEscapeString(s.Name))
		lx += 28 + float64(len([]rune(s.Name)))*11
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
