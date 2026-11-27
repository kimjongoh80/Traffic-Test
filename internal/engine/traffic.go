package engine

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	sockBufSize = 8 * 1024 * 1024 // large socket buffers for 10G/25G links

	udpMagic        = 0x5454 // "TT"
	udpTypeData     = 1
	udpTypeHello    = 2
	udpTypeProbe    = 3
	udpTypeProbeAck = 4
	// header: magic(2) type(1) rsv(1) stream(4) seq(8) sendTimeNs(8)
	udpHeaderLen = 24
)

// pacer limits a sender to a byte rate. Bursts of up to ~2 ms are allowed so
// coarse OS timers (Windows) do not cap the achievable rate.
type pacer struct {
	rate  float64 // bytes per second, 0 = unlimited
	start time.Time
	sent  float64
}

func newPacer(bitsPerSec float64) *pacer {
	return &pacer{rate: bitsPerSec / 8, start: time.Now()}
}

func (p *pacer) wait(n int, stop *atomic.Bool) {
	if p.rate <= 0 {
		return
	}
	p.sent += float64(n)
	target := p.start.Add(time.Duration(p.sent / p.rate * float64(time.Second)))
	for {
		d := time.Until(target)
		if d < 2*time.Millisecond || stop.Load() {
			return
		}
		if d > 50*time.Millisecond {
			d = 50 * time.Millisecond
		}
		time.Sleep(d)
	}
}

func tuneTCP(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(sockBufSize)
		_ = tc.SetWriteBuffer(sockBufSize)
		_ = tc.SetNoDelay(false)
	}
}

func tuneUDP(c *net.UDPConn) {
	_ = c.SetReadBuffer(sockBufSize)
	_ = c.SetWriteBuffer(sockBufSize)
}

func isClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)
}

// trafficGroup runs the senders or receivers of one side of a test.
type trafficGroup struct {
	stop     atomic.Bool
	wg       sync.WaitGroup
	counters []*streamCounter // receiver counters or sender counters
}

func newTrafficGroup(n int) *trafficGroup {
	g := &trafficGroup{}
	for i := 0; i < n; i++ {
		g.counters = append(g.counters, &streamCounter{})
	}
	return g
}

func (g *trafficGroup) totals() []StreamTotals {
	out := make([]StreamTotals, len(g.counters))
	for i, c := range g.counters {
		out[i] = StreamTotals{Stream: i, Bytes: c.bytes.Load(), Packets: c.packets.Load()}
	}
	return out
}

// ---- TCP ----

func (g *trafficGroup) startTCPSenders(conns []net.Conn, p TestParams) {
	perStream := float64(p.Bandwidth) / float64(len(conns))
	for i, c := range conns {
		g.wg.Add(1)
		go func(i int, c net.Conn) {
			defer g.wg.Done()
			buf := make([]byte, p.Length)
			for j := range buf {
				buf[j] = byte(j)
			}
			pc := newPacer(perStream)
			ctr := g.counters[i]
			for !g.stop.Load() {
				_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
				n, err := c.Write(buf)
				if n > 0 {
					ctr.bytes.Add(uint64(n))
				}
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue
					}
					return
				}
				pc.wait(n, &g.stop)
			}
		}(i, c)
	}
}

func (g *trafficGroup) startTCPReceivers(conns []net.Conn) {
	for i, c := range conns {
		g.wg.Add(1)
		go func(i int, c net.Conn) {
			defer g.wg.Done()
			buf := make([]byte, 256*1024)
			ctr := g.counters[i]
			for {
				n, err := c.Read(buf)
				if n > 0 {
					ctr.bytes.Add(uint64(n))
				}
				if err != nil {
					return
				}
			}
		}(i, c)
	}
}

// ---- UDP ----

func putUDPHeader(b []byte, typ byte, stream uint32, seq uint64, ts int64) {
	binary.BigEndian.PutUint16(b[0:2], udpMagic)
	b[2] = typ
	b[3] = 0
	binary.BigEndian.PutUint32(b[4:8], stream)
	binary.BigEndian.PutUint64(b[8:16], seq)
	binary.BigEndian.PutUint64(b[16:24], uint64(ts))
}

func parseUDPHeader(b []byte) (typ byte, stream uint32, seq uint64, ts int64, ok bool) {
	if len(b) < udpHeaderLen || binary.BigEndian.Uint16(b[0:2]) != udpMagic {
		return
	}
	return b[2], binary.BigEndian.Uint32(b[4:8]), binary.BigEndian.Uint64(b[8:16]),
		int64(binary.BigEndian.Uint64(b[16:24])), true
}

// udpTarget is where a UDP sender writes: either a connected socket (client)
// or a listening socket plus the peer address learnt from hello (server).
type udpTarget struct {
	conn *net.UDPConn
	addr *net.UDPAddr // nil when conn is connected
}

func (t udpTarget) write(b []byte) (int, error) {
	if t.addr == nil {
		return t.conn.Write(b)
	}
	return t.conn.WriteToUDP(b, t.addr)
}

func (g *trafficGroup) startUDPSenders(targets []udpTarget, p TestParams) {
	perStream := float64(p.Bandwidth) / float64(len(targets))
	epoch := time.Now()
	for i, t := range targets {
		g.wg.Add(1)
		go func(i int, t udpTarget) {
			defer g.wg.Done()
			buf := make([]byte, p.Length)
			pc := newPacer(perStream)
			ctr := g.counters[i]
			var seq uint64
			for !g.stop.Load() {
				putUDPHeader(buf, udpTypeData, uint32(i), seq, int64(time.Since(epoch)))
				n, err := t.write(buf)
				if err != nil {
					if isClosed(err) {
						return
					}
					// ENOBUFS / ICMP errors: back off briefly and continue.
					time.Sleep(100 * time.Microsecond)
					continue
				}
				seq++
				ctr.packets.Add(1)
				ctr.bytes.Add(uint64(n))
				pc.wait(n, &g.stop)
			}
		}(i, t)
	}
}

// startUDPReceivers reads data packets on each socket. When onHello is set it
// is called with the source address of hello packets (server side).
func (g *trafficGroup) startUDPReceivers(conns []*net.UDPConn, onHello func(i int, a *net.UDPAddr)) {
	epoch := time.Now()
	for i, c := range conns {
		g.wg.Add(1)
		go func(i int, c *net.UDPConn) {
			defer g.wg.Done()
			buf := make([]byte, 65536)
			ctr := g.counters[i]
			var expected uint64
			var prevTransit int64
			var havePrev bool
			var jitter float64
			for {
				n, addr, err := c.ReadFromUDP(buf)
				if err != nil {
					if isClosed(err) || g.stop.Load() {
						return
					}
					continue // e.g. WSAECONNRESET on Windows
				}
				typ, _, seq, ts, ok := parseUDPHeader(buf[:n])
				if !ok {
					continue
				}
				switch typ {
				case udpTypeHello:
					if onHello != nil {
						onHello(i, addr)
					}
					continue
				case udpTypeData:
				default:
					continue
				}
				ctr.packets.Add(1)
				ctr.bytes.Add(uint64(n))
				if seq+1 > expected {
					expected = seq + 1
					ctr.expected.Store(expected)
				} else {
					ctr.outOfOrder.Add(1)
				}
				// RFC 3550 interarrival jitter; clock offset cancels out.
				transit := int64(time.Since(epoch)) - ts
				if havePrev {
					d := math.Abs(float64(transit - prevTransit))
					jitter += (d - jitter) / 16
					ctr.setJitter(jitter / 1e6)
				}
				prevTransit, havePrev = transit, true
			}
		}(i, c)
	}
}
