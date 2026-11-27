package engine

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

func itoa(i int) string { return strconv.Itoa(i) }

// Server accepts tests from clients. It runs one test at a time.
type Server struct {
	Port   int
	Bind   string // optional bind address
	Logger *log.Logger

	busy    sync.Mutex
	mu      sync.Mutex
	pending map[string]chan dataConn
	ln      net.Listener
	probe   *net.UDPConn
}

type dataConn struct {
	idx  int
	conn net.Conn
}

func (s *Server) logf(format string, a ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, a...)
	}
}

// ListenAndServe blocks until Close is called or a fatal error occurs.
func (s *Server) ListenAndServe() error {
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	addr := net.JoinHostPort(s.Bind, itoa(s.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	uaddr, _ := net.ResolveUDPAddr("udp", addr)
	pc, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		ln.Close()
		return fmt.Errorf("latency probe port: %w", err)
	}
	s.mu.Lock()
	s.ln, s.probe = ln, pc
	s.pending = map[string]chan dataConn{}
	s.mu.Unlock()
	go runProbeEcho(pc)
	s.logf("server listening on TCP/UDP %s (UDP data ports %d+)", addr, s.Port+1)
	for {
		c, err := ln.Accept()
		if err != nil {
			if isClosed(err) {
				return nil
			}
			s.logf("accept: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go s.handleConn(c)
	}
}

// Close stops the server.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probe != nil {
		s.probe.Close()
	}
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

func (s *Server) handleConn(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var kind [1]byte
	if _, err := io.ReadFull(c, kind[:]); err != nil {
		c.Close()
		return
	}
	switch kind[0] {
	case connControl:
		defer c.Close()
		if err := s.handleControl(c); err != nil {
			s.logf("test from %s failed: %v", c.RemoteAddr(), err)
		}
	case connData:
		var hdr [sessionLen + 2]byte
		if _, err := io.ReadFull(c, hdr[:]); err != nil {
			c.Close()
			return
		}
		_ = c.SetReadDeadline(time.Time{})
		sess := hex.EncodeToString(hdr[:sessionLen])
		idx := int(binary.BigEndian.Uint16(hdr[sessionLen:]))
		s.mu.Lock()
		ch := s.pending[sess]
		s.mu.Unlock()
		if ch == nil {
			c.Close()
			return
		}
		tuneTCP(c)
		select {
		case ch <- dataConn{idx, c}:
		default:
			c.Close()
		}
	default:
		c.Close()
	}
}

func (s *Server) handleControl(raw net.Conn) error {
	cc := newCtrlConn(raw)
	m, err := cc.recv(10 * time.Second)
	if err != nil {
		return err
	}
	if m.Type != "start" || m.Params == nil {
		return fmt.Errorf("unexpected message %q", m.Type)
	}
	if m.Version != ProtocolVersion {
		_ = cc.send(ctrlMsg{Type: "error", Error: fmt.Sprintf("protocol version mismatch (server %d, client %d)", ProtocolVersion, m.Version)})
		return fmt.Errorf("version mismatch")
	}
	if !s.busy.TryLock() {
		_ = cc.send(ctrlMsg{Type: "error", Error: "server is busy with another test"})
		return fmt.Errorf("busy")
	}
	defer s.busy.Unlock()

	p := *m.Params
	if err := p.Normalize(); err != nil {
		_ = cc.send(ctrlMsg{Type: "error", Error: err.Error()})
		return err
	}
	dir := "client->server"
	if p.Reverse {
		dir = "server->client"
	}
	s.logf("test from %s: %s %s, %d stream(s), %.0fs", raw.RemoteAddr(), p.Protocol, dir, p.Streams, p.DurationSec)

	var sid [sessionLen]byte
	_, _ = rand.Read(sid[:])
	sess := hex.EncodeToString(sid[:])
	host, _ := os.Hostname()
	ready := ctrlMsg{Type: "ready", Session: sess, Hostname: host}

	var tcpConns []net.Conn
	var udpConns []*net.UDPConn
	defer func() {
		for _, c := range tcpConns {
			if c != nil {
				c.Close()
			}
		}
		for _, c := range udpConns {
			c.Close()
		}
	}()

	var dataCh chan dataConn
	if p.Protocol == "tcp" {
		dataCh = make(chan dataConn, p.Streams)
		s.mu.Lock()
		s.pending[sess] = dataCh
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.pending, sess)
			s.mu.Unlock()
		}()
	} else {
		bind := s.Bind
		for i := 0; i < p.Streams; i++ {
			port := s.Port + 1 + i
			ua, _ := net.ResolveUDPAddr("udp", net.JoinHostPort(bind, itoa(port)))
			uc, err := net.ListenUDP("udp", ua)
			if err != nil {
				_ = cc.send(ctrlMsg{Type: "error", Error: fmt.Sprintf("cannot open UDP port %d: %v", port, err)})
				return err
			}
			tuneUDP(uc)
			udpConns = append(udpConns, uc)
			ready.UDPPorts = append(ready.UDPPorts, port)
		}
	}
	if err := cc.send(ready); err != nil {
		return err
	}

	group := newTrafficGroup(p.Streams)
	serverReceives := !p.Reverse

	// Collect data connections / peer addresses.
	var udpPeers []*net.UDPAddr
	var helloMu sync.Mutex
	helloDone := make(chan struct{})
	if p.Protocol == "tcp" {
		tcpConns = make([]net.Conn, p.Streams)
		deadline := time.After(10 * time.Second)
		for got := 0; got < p.Streams; {
			select {
			case dc := <-dataCh:
				if dc.idx < 0 || dc.idx >= p.Streams || tcpConns[dc.idx] != nil {
					dc.conn.Close()
					continue
				}
				tcpConns[dc.idx] = dc.conn
				got++
			case <-deadline:
				_ = cc.send(ctrlMsg{Type: "error", Error: "timed out waiting for data connections"})
				return fmt.Errorf("data connection timeout")
			}
		}
	} else {
		udpPeers = make([]*net.UDPAddr, p.Streams)
		seen := 0
		onHello := func(i int, a *net.UDPAddr) {
			helloMu.Lock()
			defer helloMu.Unlock()
			if udpPeers[i] == nil {
				udpPeers[i] = a
				seen++
				if seen == p.Streams {
					close(helloDone)
				}
			}
		}
		// Receivers also catch hello packets; in reverse mode their data
		// counters simply stay at zero.
		group.startUDPReceivers(udpConns, onHello)
	}

	m, err = cc.recv(15 * time.Second)
	if err != nil {
		return err
	}
	if m.Type != "begin" {
		return fmt.Errorf("expected begin, got %q", m.Type)
	}
	if p.Protocol == "udp" {
		select {
		case <-helloDone:
		case <-time.After(5 * time.Second):
			_ = cc.send(ctrlMsg{Type: "error", Error: "no UDP packets reached the server (check firewall for UDP ports)"})
			return fmt.Errorf("udp hello timeout")
		}
	}
	if err := cc.send(ctrlMsg{Type: "go"}); err != nil {
		return err
	}
	start := time.Now()

	sendGroup := group
	if !serverReceives {
		sendGroup = newTrafficGroup(p.Streams)
		if p.Protocol == "tcp" {
			sendGroup.startTCPSenders(tcpConns, p)
		} else {
			targets := make([]udpTarget, p.Streams)
			for i := range targets {
				targets[i] = udpTarget{conn: udpConns[i], addr: udpPeers[i]}
			}
			sendGroup.startUDPSenders(targets, p)
		}
	} else if p.Protocol == "tcp" {
		group.startTCPReceivers(tcpConns)
	}

	// Wait for "end" while streaming interval reports if we are the receiver.
	endCh := make(chan error, 1)
	go func() {
		for {
			m, err := cc.recv(time.Duration(p.DurationSec*float64(time.Second)) + 30*time.Second)
			if err != nil {
				endCh <- err
				return
			}
			if m.Type == "end" {
				endCh <- nil
				return
			}
		}
	}()

	var smp *sampler
	var tick *time.Ticker
	var tickC <-chan time.Time
	if serverReceives {
		smp = newSampler(p.Protocol == "udp", group.counters)
		smp.begin(start)
		tick = time.NewTicker(time.Duration(p.IntervalSec * float64(time.Second)))
		defer tick.Stop()
		tickC = tick.C
	}

	for {
		select {
		case now := <-tickC:
			iv := smp.sample(now)
			if err := cc.send(ctrlMsg{Type: "interval", Interval: &iv}); err != nil {
				return err
			}
			continue
		case err := <-endCh:
			if err != nil {
				sendGroup.stop.Store(true)
				return err
			}
		}
		break
	}

	if serverReceives {
		// Let in-flight packets arrive, then report the final partial interval.
		time.Sleep(250 * time.Millisecond)
		if iv, ok := smp.final(time.Now(), start.Add(time.Duration(p.DurationSec*float64(time.Second)))); ok {
			if err := cc.send(ctrlMsg{Type: "interval", Interval: &iv}); err != nil {
				return err
			}
		}
		group.stop.Store(true)
		_ = cc.send(ctrlMsg{Type: "result", Totals: group.totals()})
	} else {
		sendGroup.stop.Store(true)
		_ = cc.send(ctrlMsg{Type: "result", Totals: sendGroup.totals()})
	}
	s.logf("test from %s finished after %.1fs", raw.RemoteAddr(), time.Since(start).Seconds())
	return nil
}
