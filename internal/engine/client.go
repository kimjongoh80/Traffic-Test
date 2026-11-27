package engine

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"time"
)

// RunClient runs one test against a server. onInterval (optional) is called
// for each report interval as it completes. Cancelling ctx ends the test early
// and still returns the results gathered so far.
func RunClient(ctx context.Context, host string, port int, p TestParams, onInterval func(Interval)) (*Result, error) {
	if port == 0 {
		port = DefaultPort
	}
	if err := p.Normalize(); err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(host, itoa(port))
	d := net.Dialer{Timeout: 5 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to server %s: %w", addr, err)
	}
	defer raw.Close()
	if _, err := raw.Write([]byte{connControl}); err != nil {
		return nil, err
	}
	cc := newCtrlConn(raw)
	clientHost, _ := os.Hostname()
	if err := cc.send(ctrlMsg{Type: "start", Version: ProtocolVersion, Params: &p, Hostname: clientHost}); err != nil {
		return nil, err
	}
	ready, err := cc.recv(10 * time.Second)
	if err != nil {
		return nil, err
	}
	if ready.Type != "ready" {
		return nil, fmt.Errorf("unexpected reply %q", ready.Type)
	}
	sid, err := hex.DecodeString(ready.Session)
	if err != nil || len(sid) != sessionLen {
		return nil, fmt.Errorf("bad session id")
	}

	res := &Result{Params: p, Server: addr, ServerHost: ready.Hostname, ClientHost: clientHost}
	clientReceives := p.Reverse

	// Open data channels.
	var tcpConns []net.Conn
	var udpConns []*net.UDPConn
	defer func() {
		for _, c := range tcpConns {
			c.Close()
		}
		for _, c := range udpConns {
			c.Close()
		}
	}()
	if p.Protocol == "tcp" {
		for i := 0; i < p.Streams; i++ {
			c, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				return nil, fmt.Errorf("data connection %d: %w", i, err)
			}
			tuneTCP(c)
			hdr := make([]byte, 1+sessionLen+2)
			hdr[0] = connData
			copy(hdr[1:], sid)
			binary.BigEndian.PutUint16(hdr[1+sessionLen:], uint16(i))
			if _, err := c.Write(hdr); err != nil {
				c.Close()
				return nil, err
			}
			tcpConns = append(tcpConns, c)
		}
	} else {
		if len(ready.UDPPorts) != p.Streams {
			return nil, fmt.Errorf("server offered %d UDP ports, need %d", len(ready.UDPPorts), p.Streams)
		}
		for _, up := range ready.UDPPorts {
			ra, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, itoa(up)))
			if err != nil {
				return nil, err
			}
			uc, err := net.DialUDP("udp", nil, ra)
			if err != nil {
				return nil, err
			}
			tuneUDP(uc)
			udpConns = append(udpConns, uc)
		}
	}

	group := newTrafficGroup(p.Streams)
	if clientReceives {
		if p.Protocol == "tcp" {
			group.startTCPReceivers(tcpConns)
		} else {
			group.startUDPReceivers(udpConns, nil)
		}
	}
	if p.Protocol == "udp" {
		// Hello packets let the server learn our address (works through NAT).
		hello := make([]byte, udpHeaderLen)
		for r := 0; r < 5; r++ {
			for i, uc := range udpConns {
				putUDPHeader(hello, udpTypeHello, uint32(i), 0, 0)
				_, _ = uc.Write(hello)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	var prb *prober
	if p.LatencyMs > 0 {
		prb, err = startProber(host, port, time.Duration(p.LatencyMs)*time.Millisecond)
		if err != nil {
			return nil, fmt.Errorf("latency prober: %w", err)
		}
	}
	stopProber := func() {
		if prb != nil {
			res.Summary.Latency = prb.stop()
			prb = nil
		}
	}
	defer stopProber()

	if err := cc.send(ctrlMsg{Type: "begin"}); err != nil {
		return nil, err
	}
	if m, err := cc.recv(15 * time.Second); err != nil {
		return nil, err
	} else if m.Type != "go" {
		return nil, fmt.Errorf("expected go, got %q", m.Type)
	}
	start := time.Now()
	res.StartedAt = start
	duration := time.Duration(p.DurationSec * float64(time.Second))
	planned := start.Add(duration)

	emit := func(iv Interval) {
		if prb != nil {
			iv.Latency = prb.takeWindow()
		}
		res.Intervals = append(res.Intervals, iv)
		if onInterval != nil {
			onInterval(iv)
		}
		// Mirror to the server so its screen can follow the test.
		_ = cc.send(ctrlMsg{Type: "client_interval", Interval: &iv})
	}

	// Control reader: interval reports and the final result from the server.
	type ctrlEvent struct {
		m   ctrlMsg
		err error
	}
	ctrlCh := make(chan ctrlEvent, 64)
	go func() {
		for {
			m, err := cc.recv(duration + 30*time.Second)
			ctrlCh <- ctrlEvent{m, err}
			if err != nil || m.Type == "result" {
				return
			}
		}
	}()

	if !clientReceives {
		if p.Protocol == "tcp" {
			group.startTCPSenders(tcpConns, p)
		} else {
			t := make([]udpTarget, len(udpConns))
			for i, uc := range udpConns {
				t[i] = udpTarget{conn: uc}
			}
			group.startUDPSenders(t, p)
		}
	}

	var smp *sampler
	var tickC <-chan time.Time
	if clientReceives {
		smp = newSampler(p.Protocol == "udp", group.counters)
		smp.begin(start)
		tk := time.NewTicker(time.Duration(p.IntervalSec * float64(time.Second)))
		defer tk.Stop()
		tickC = tk.C
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()

	var remote []StreamTotals
	var runErr error
loop:
	for {
		select {
		case now := <-tickC:
			emit(smp.sample(now))
		case ev := <-ctrlCh:
			if ev.err != nil {
				runErr = ev.err
				break loop
			}
			if ev.m.Type == "interval" && ev.m.Interval != nil {
				emit(*ev.m.Interval)
			}
		case <-timer.C:
			break loop
		case <-ctx.Done():
			planned = time.Now()
			break loop
		}
	}
	if runErr != nil {
		group.stop.Store(true)
		return nil, runErr
	}

	// Finish: stop our senders, tell the server, collect its totals.
	if !clientReceives {
		group.stop.Store(true)
		group.wg.Wait()
	}
	if err := cc.send(ctrlMsg{Type: "end"}); err != nil {
		return nil, err
	}
	if clientReceives {
		time.Sleep(250 * time.Millisecond)
		if iv, ok := smp.final(time.Now(), planned); ok {
			emit(iv)
		}
		group.stop.Store(true)
	}
	resultTimeout := time.After(10 * time.Second)
wait:
	for {
		select {
		case ev := <-ctrlCh:
			if ev.err != nil {
				return nil, ev.err
			}
			switch ev.m.Type {
			case "interval":
				if ev.m.Interval != nil {
					emit(*ev.m.Interval)
				}
			case "result":
				remote = ev.m.Totals
				break wait
			}
		case <-resultTimeout:
			return nil, fmt.Errorf("timed out waiting for server results")
		}
	}
	stopProber()

	var sender, receiver []StreamTotals
	if clientReceives {
		sender, receiver = remote, group.totals()
	} else {
		sender, receiver = group.totals(), remote
	}
	summarize(res, sender, receiver)
	_ = cc.send(ctrlMsg{Type: "report", Result: res})
	return res, nil
}

func summarize(res *Result, sender, receiver []StreamTotals) {
	s := &res.Summary
	udp := res.Params.Protocol == "udp"
	for i, iv := range res.Intervals {
		s.DurationSec += iv.End - iv.Start
		bps := iv.Sum.BitsPerSec
		// Ignore a short final partial interval for min/max.
		if i == len(res.Intervals)-1 && i > 0 && iv.End-iv.Start < 0.5*res.Params.IntervalSec {
			continue
		}
		if i == 0 || bps < s.MinBitsPerSec {
			s.MinBitsPerSec = bps
		}
		if bps > s.MaxBitsPerSec {
			s.MaxBitsPerSec = bps
		}
	}
	var jitterSum float64
	var jitterN int
	for i := range receiver {
		r := receiver[i]
		st := StreamInterval{Stream: i, Bytes: r.Bytes, Packets: r.Packets}
		if s.DurationSec > 0 {
			st.BitsPerSec = float64(r.Bytes) * 8 / s.DurationSec
		}
		var sent StreamTotals
		if i < len(sender) {
			sent = sender[i]
		}
		s.SentBytes += sent.Bytes
		if udp {
			if sent.Packets > r.Packets {
				st.Lost = sent.Packets - r.Packets
			}
			if sent.Packets > 0 {
				st.LossPct = float64(st.Lost) * 100 / float64(sent.Packets)
			}
			s.PacketsSent += sent.Packets
			s.PacketsReceived += r.Packets
			s.Lost += st.Lost
		}
		s.Bytes += r.Bytes
		s.Streams = append(s.Streams, st)
	}
	if udp {
		for _, iv := range res.Intervals {
			s.OutOfOrder += iv.Sum.OutOfOrder
			if iv.Sum.Packets > 0 {
				jitterSum += iv.Sum.JitterMs
				jitterN++
			}
		}
		if jitterN > 0 {
			s.JitterMs = jitterSum / float64(jitterN)
		}
		if s.PacketsSent > 0 {
			s.LossPct = float64(s.Lost) * 100 / float64(s.PacketsSent)
		}
		// Per-stream out-of-order and jitter from intervals.
		for i := range s.Streams {
			var js float64
			var jn int
			for _, iv := range res.Intervals {
				if i < len(iv.Streams) {
					s.Streams[i].OutOfOrder += iv.Streams[i].OutOfOrder
					if iv.Streams[i].Packets > 0 {
						js += iv.Streams[i].JitterMs
						jn++
					}
				}
			}
			if jn > 0 {
				s.Streams[i].JitterMs = js / float64(jn)
			}
		}
	}
	if s.DurationSec > 0 {
		s.BitsPerSec = float64(s.Bytes) * 8 / s.DurationSec
	}
}
