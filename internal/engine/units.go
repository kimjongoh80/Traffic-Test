package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseBits parses a rate such as "500M", "1G", "25g", "100k" or "1000000"
// into bits per second (decimal multipliers, like network gear).
func ParseBits(s string) (uint64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSuffix(s, "bps")
	s = strings.TrimSuffix(s, "b")
	if s == "" || s == "0" {
		return 0, nil
	}
	mult := 1.0
	switch s[len(s)-1] {
	case 'k':
		mult = 1e3
	case 'm':
		mult = 1e6
	case 'g':
		mult = 1e9
	case 't':
		mult = 1e12
	}
	if mult != 1 {
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid rate %q", s)
	}
	return uint64(v * mult), nil
}

// FormatBits renders bits per second with a unit, e.g. "941.53 Mbps".
func FormatBits(bps float64) string {
	switch {
	case bps >= 1e9:
		return fmt.Sprintf("%.2f Gbps", bps/1e9)
	case bps >= 1e6:
		return fmt.Sprintf("%.2f Mbps", bps/1e6)
	case bps >= 1e3:
		return fmt.Sprintf("%.2f Kbps", bps/1e3)
	}
	return fmt.Sprintf("%.0f bps", bps)
}

// FormatBytes renders a byte count with a binary unit, e.g. "1.10 GiB".
func FormatBytes(b uint64) string {
	f := float64(b)
	switch {
	case f >= 1<<30:
		return fmt.Sprintf("%.2f GiB", f/(1<<30))
	case f >= 1<<20:
		return fmt.Sprintf("%.2f MiB", f/(1<<20))
	case f >= 1<<10:
		return fmt.Sprintf("%.2f KiB", f/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}
