package engine

import (
	"net"
	"sync"
	"time"
)

// runProbeEcho answers latency probes on the server's UDP control port.
func runProbeEcho(c *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		n, addr, err := c.ReadFromUDP(buf)
		if err != nil {
			if isClosed(err) {
				return
			}
			continue
		}
		typ, _, _, _, ok := parseUDPHeader(buf[:n])
		if !ok || typ != udpTypeProbe {
			continue
		}
		buf[2] = udpTypeProbeAck
		_, _ = c.WriteToUDP(buf[:n], addr)
	}
}

// prober sends small UDP probes to the server and records round-trip times.
// It runs alongside the traffic, so it measures latency under load.
type prober struct {
	conn     *net.UDPConn
	interval time.Duration
	epoch    time.Time

	mu         sync.Mutex
	sent       int
	windowSent int
	window     []float64
	all        []float64
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

func startProber(serverHost string, port int, interval time.Duration) (*prober, error) {
	raddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(serverHost, itoa(port)))
	if err != nil {
		return nil, err
	}
	c, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}
	p := &prober{conn: c, interval: interval, epoch: time.Now(), stopCh: make(chan struct{})}
	p.wg.Add(2)
	go p.sendLoop()
	go p.recvLoop()
	return p, nil
}

func (p *prober) sendLoop() {
	defer p.wg.Done()
	t := time.NewTicker(p.interval)
	defer t.Stop()
	buf := make([]byte, 64)
	var seq uint64
	for {
		putUDPHeader(buf, udpTypeProbe, 0, seq, int64(time.Since(p.epoch)))
		if _, err := p.conn.Write(buf); err == nil {
			p.mu.Lock()
			p.sent++
			p.windowSent++
			p.mu.Unlock()
		}
		seq++
		select {
		case <-p.stopCh:
			return
		case <-t.C:
		}
	}
}

func (p *prober) recvLoop() {
	defer p.wg.Done()
	buf := make([]byte, 2048)
	for {
		n, err := p.conn.Read(buf)
		if err != nil {
			if isClosed(err) {
				return
			}
			select {
			case <-p.stopCh:
				return
			default:
			}
			continue
		}
		typ, _, _, ts, ok := parseUDPHeader(buf[:n])
		if !ok || typ != udpTypeProbeAck {
			continue
		}
		rtt := float64(int64(time.Since(p.epoch))-ts) / 1e6
		p.mu.Lock()
		p.window = append(p.window, rtt)
		p.all = append(p.all, rtt)
		p.mu.Unlock()
	}
}

// takeWindow returns stats for probes answered since the previous call.
func (p *prober) takeWindow() *LatencyStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	ls := latencyFrom(p.windowSent, p.window)
	p.window = p.window[:0]
	p.windowSent = 0
	return ls
}

// stop waits briefly for in-flight replies, then returns overall stats.
func (p *prober) stop() *LatencyStats {
	close(p.stopCh)
	time.Sleep(300 * time.Millisecond)
	_ = p.conn.Close()
	p.wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	return latencyFrom(p.sent, p.all)
}
