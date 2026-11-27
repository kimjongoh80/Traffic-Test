// Package engine implements the Traffic-Test measurement engine: a control
// protocol between a client and a server, TCP/UDP traffic generators and
// receivers, and latency probing. It is shared by the CLI and the web GUI.
package engine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const (
	// ProtocolVersion is bumped whenever the control protocol changes.
	ProtocolVersion = 1
	// DefaultPort is the TCP control/data port and the UDP latency-probe port.
	// UDP data streams use DefaultPort+1 .. DefaultPort+N.
	DefaultPort = 5210

	connControl byte = 'C'
	connData    byte = 'D'

	sessionLen = 16
)

// TestParams describes one test run. The client sends it to the server.
type TestParams struct {
	Protocol    string  `json:"protocol"`     // "tcp" or "udp"
	Reverse     bool    `json:"reverse"`      // true: server sends, client receives
	DurationSec float64 `json:"duration_sec"` // test length in seconds
	Streams     int     `json:"streams"`      // parallel streams
	Bandwidth   uint64  `json:"bandwidth"`    // target total bits/s, 0 = unlimited
	Length      int     `json:"length"`       // TCP write size / UDP payload size in bytes
	IntervalSec float64 `json:"interval_sec"` // report interval in seconds
	LatencyMs   int     `json:"latency_ms"`   // latency probe interval in ms, 0 = disabled
	// Criteria travel with the test so the server side can show the same
	// pass/fail limits. Optional.
	Criteria *Criteria `json:"criteria,omitempty"`
}

// Normalize fills defaults and validates the parameters.
func (p *TestParams) Normalize() error {
	if p.Protocol == "" {
		p.Protocol = "tcp"
	}
	if p.Protocol != "tcp" && p.Protocol != "udp" {
		return fmt.Errorf("unknown protocol %q", p.Protocol)
	}
	if p.DurationSec <= 0 {
		p.DurationSec = 10
	}
	if p.DurationSec > 86400 {
		return fmt.Errorf("duration too long")
	}
	if p.Streams <= 0 {
		p.Streams = 1
	}
	if p.Streams > 128 {
		return fmt.Errorf("too many streams (max 128)")
	}
	if p.IntervalSec <= 0 {
		p.IntervalSec = 1
	}
	if p.IntervalSec < 0.1 {
		p.IntervalSec = 0.1
	}
	if p.Protocol == "udp" {
		if p.Length <= 0 {
			p.Length = 1460
		}
		if p.Length < udpHeaderLen {
			p.Length = udpHeaderLen
		}
		if p.Length > 65507 {
			p.Length = 65507
		}
		if p.Bandwidth == 0 {
			p.Bandwidth = 1_000_000_000 // UDP defaults to 1 Gbps
		}
	} else {
		if p.Length <= 0 {
			p.Length = 128 * 1024
		}
		if p.Length > 16*1024*1024 {
			p.Length = 16 * 1024 * 1024
		}
	}
	if p.LatencyMs < 0 {
		p.LatencyMs = 0
	}
	return nil
}

// ctrlMsg is a JSON line exchanged on the control connection.
type ctrlMsg struct {
	Type     string          `json:"type"` // start, ready, begin, go, interval, end, result, error
	Version  int             `json:"version,omitempty"`
	Params   *TestParams     `json:"params,omitempty"`
	Session  string          `json:"session,omitempty"`
	UDPPorts []int           `json:"udp_ports,omitempty"`
	Error    string          `json:"error,omitempty"`
	Interval *Interval       `json:"interval,omitempty"`
	Totals   []StreamTotals  `json:"totals,omitempty"`
	Hostname string          `json:"hostname,omitempty"`
	Result   *Result         `json:"result,omitempty"`
	Extra    json.RawMessage `json:"extra,omitempty"`
}

// StreamTotals are the end-of-test counters of one side for one stream.
type StreamTotals struct {
	Stream  int    `json:"stream"`
	Bytes   uint64 `json:"bytes"`
	Packets uint64 `json:"packets"`
}

type ctrlConn struct {
	c  net.Conn
	r  *bufio.Reader
	en *json.Encoder
}

func newCtrlConn(c net.Conn) *ctrlConn {
	return &ctrlConn{c: c, r: bufio.NewReaderSize(c, 64*1024), en: json.NewEncoder(c)}
}

func (cc *ctrlConn) send(m ctrlMsg) error {
	_ = cc.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return cc.en.Encode(m)
}

func (cc *ctrlConn) recv(timeout time.Duration) (ctrlMsg, error) {
	var m ctrlMsg
	if timeout > 0 {
		_ = cc.c.SetReadDeadline(time.Now().Add(timeout))
	} else {
		_ = cc.c.SetReadDeadline(time.Time{})
	}
	line, err := cc.r.ReadBytes('\n')
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return m, fmt.Errorf("bad control message: %w", err)
	}
	if m.Type == "error" {
		return m, fmt.Errorf("remote error: %s", m.Error)
	}
	return m, nil
}
