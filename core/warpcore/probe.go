package warpcore

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Diniboy1123/usque/api"
	"golang.zx2c4.com/wireguard/tun"
)

// The probes bring up a complete tunnel on an in-memory device, push a DNS
// query for www.cloudflare.com to 1.1.1.1 through it and wait for the answer.
// An answer proves real traffic made the round trip, which a handshake alone
// does not: UC-SECURE completes WireGuard handshakes and then drops every
// data packet.

type probeResult struct {
	OK     bool   `json:"ok"`
	Millis int64  `json:"ms"`
	Detail string `json:"detail"`
}

func (r probeResult) json(start time.Time) string {
	r.Millis = time.Since(start).Milliseconds()
	b, _ := json.Marshal(r)
	return string(b)
}

// ProbeWireGuard tests WireGuard on one UDP port. Returns JSON {ok, ms, detail}.
// Must not run while a WireGuard tunnel with the same key is up.
func ProbeWireGuard(accountJSON string, port int, timeoutMs int) string {
	start := time.Now()
	acct, err := parseWg(accountJSON)
	if err != nil {
		return probeResult{Detail: err.Error()}.json(start)
	}
	src, err := netip.ParseAddr(acct.AddressV4)
	if err != nil {
		return probeResult{Detail: "bad tunnel address " + acct.AddressV4}.json(start)
	}
	mem := newMemTun()
	dev, err := newWgDevice(mem, acct, port, "probe")
	if err != nil {
		return probeResult{Detail: err.Error()}.json(start)
	}
	r := dnsRoundTrip(mem, src, time.Duration(timeoutMs)*time.Millisecond)
	if !r.OK {
		if wgHandshakeDone(dev) {
			r.Detail = "handshake OK, but no data came back (network blocks WireGuard traffic)"
		} else {
			r.Detail = "no handshake (this UDP port is blocked)"
		}
	}
	dev.Close()
	log.Printf("probe wireguard-%d: ok=%v %s", port, r.OK, r.Detail)
	return r.json(start)
}

// ProbeMasque tests MASQUE over HTTP/3 (UDP 443) or HTTP/2 (TCP 443).
// Returns JSON {ok, ms, detail}.
func ProbeMasque(accountJSON string, http2 bool, timeoutMs int) string {
	start := time.Now()
	acct, err := parseMasque(accountJSON)
	if err != nil {
		return probeResult{Detail: err.Error()}.json(start)
	}
	src, err := netip.ParseAddr(acct.IPv4)
	if err != nil {
		return probeResult{Detail: "bad tunnel address " + acct.IPv4}.json(start)
	}
	cfg, err := masqueConfig(acct, http2)
	if err != nil {
		return probeResult{Detail: err.Error()}.json(start)
	}
	mem := newMemTun()
	cfg.Device = mem
	mark := logMark()
	ctx, cancel := context.WithCancel(context.Background())
	masqueUp.Store(false)
	done := make(chan struct{})
	go func() {
		api.MaintainTunnel(ctx, cfg)
		close(done)
	}()

	r := dnsRoundTrip(mem, src, time.Duration(timeoutMs)*time.Millisecond)
	connected := masqueUp.Load()
	cancel()
	_ = mem.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		log.Printf("probe: MASQUE tunnel still shutting down, leaving it")
	}
	masqueUp.Store(false)

	if !r.OK {
		failed := lastLineSince(mark, "Failed to connect tunnel")
		switch {
		case connected:
			r.Detail = "connected, but no data came back"
		case strings.Contains(failed, "different public key"):
			r.Detail = "the network intercepted the connection with a fake certificate (refused)"
		case failed != "":
			r.Detail = "could not reach the MASQUE server (" + shorten(failed) + ")"
		default:
			r.Detail = "MASQUE server did not answer in time"
		}
	}
	log.Printf("probe %s: ok=%v %s", masqueMode(http2), r.OK, r.Detail)
	return r.json(start)
}

// dnsRoundTrip sends a DNS query through the tunnel (resending every 700 ms)
// until 1.1.1.1 answers or the timeout passes.
func dnsRoundTrip(mem *memTun, src netip.Addr, timeout time.Duration) probeResult {
	var rnd [2]byte
	_, _ = rand.Read(rnd[:])
	id := binary.BigEndian.Uint16(rnd[:])
	sport := 40000 + id%20000
	query := dnsQuery(src, id, sport, "www.cloudflare.com")

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	resend := time.NewTicker(700 * time.Millisecond)
	defer resend.Stop()
	mem.inject(query)
	for {
		select {
		case pkt := <-mem.in:
			if isDNSAnswer(pkt, id, sport) {
				return probeResult{OK: true, Detail: "DNS answered through the tunnel"}
			}
		case <-resend.C:
			mem.inject(query)
		case <-deadline.C:
			return probeResult{Detail: "no data came back"}
		}
	}
}

var dnsServerV4 = [4]byte{1, 1, 1, 1}

// dnsQuery builds an IPv4/UDP packet asking 1.1.1.1 for name's A record.
func dnsQuery(src netip.Addr, id, sport uint16, name string) []byte {
	dns := binary.BigEndian.AppendUint16(nil, id)
	dns = append(dns, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0) // RD, 1 question
	for _, label := range strings.Split(name, ".") {
		dns = append(dns, byte(len(label)))
		dns = append(dns, label...)
	}
	dns = append(dns, 0, 0, 1, 0, 1) // root, type A, class IN

	udp := binary.BigEndian.AppendUint16(nil, sport)
	udp = binary.BigEndian.AppendUint16(udp, 53)
	udp = binary.BigEndian.AppendUint16(udp, uint16(8+len(dns)))
	udp = append(udp, 0, 0) // checksum optional over IPv4
	udp = append(udp, dns...)

	ip := []byte{0x45, 0, 0, 0, byte(id >> 8), byte(id), 0, 0, 64, 17, 0, 0}
	binary.BigEndian.PutUint16(ip[2:], uint16(20+len(udp)))
	s4 := src.As4()
	ip = append(ip, s4[:]...)
	ip = append(ip, dnsServerV4[:]...)
	binary.BigEndian.PutUint16(ip[10:], ipChecksum(ip))
	return append(ip, udp...)
}

func ipChecksum(hdr []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(hdr); i += 2 {
		sum += uint32(hdr[i])<<8 | uint32(hdr[i+1])
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// isDNSAnswer reports whether pkt is 1.1.1.1's reply to our query with at
// least one answer record.
func isDNSAnswer(pkt []byte, id, sport uint16) bool {
	if len(pkt) < 20 || pkt[0]>>4 != 4 || pkt[9] != 17 {
		return false
	}
	ihl := int(pkt[0]&0x0f) * 4
	if len(pkt) < ihl+8+12 || [4]byte(pkt[12:16]) != dnsServerV4 {
		return false
	}
	udp := pkt[ihl:]
	if binary.BigEndian.Uint16(udp[0:]) != 53 || binary.BigEndian.Uint16(udp[2:]) != sport {
		return false
	}
	dns := udp[8:]
	return binary.BigEndian.Uint16(dns[0:]) == id && dns[2]&0x80 != 0 && binary.BigEndian.Uint16(dns[6:]) > 0
}

// memTun is a tunnel device with nothing behind it: the probe injects raw IP
// packets and reads the replies. It satisfies tun.Device (wireguard-go) and
// api.TunnelDevice (usque). Data channels are never closed, so a late write
// from a tunnel goroutine can't panic.
type memTun struct {
	out    chan []byte // probe -> tunnel
	in     chan []byte // tunnel -> probe
	events chan tun.Event
	done   chan struct{}
	once   sync.Once
}

func newMemTun() *memTun {
	t := &memTun{
		out:    make(chan []byte, 16),
		in:     make(chan []byte, 64),
		events: make(chan tun.Event, 1),
		done:   make(chan struct{}),
	}
	t.events <- tun.EventUp
	return t
}

func (t *memTun) inject(pkt []byte) {
	select {
	case t.out <- pkt:
	default:
	}
}

func (t *memTun) deliver(pkt []byte) {
	select {
	case t.in <- append([]byte(nil), pkt...):
	default: // full: drop, as a real link would
	}
}

func (t *memTun) File() *os.File           { return nil }
func (t *memTun) MTU() (int, error)        { return tunnelMTU, nil }
func (t *memTun) Name() (string, error)    { return "probe", nil }
func (t *memTun) Events() <-chan tun.Event { return t.events }
func (t *memTun) BatchSize() int           { return 1 }

func (t *memTun) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	select {
	case pkt := <-t.out:
		sizes[0] = copy(bufs[0][offset:], pkt)
		return 1, nil
	case <-t.done:
		return 0, os.ErrClosed
	}
}

func (t *memTun) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		t.deliver(b[offset:])
	}
	return len(bufs), nil
}

func (t *memTun) ReadPacket(buf []byte) (int, error) {
	select {
	case pkt := <-t.out:
		return copy(buf, pkt), nil
	case <-t.done:
		return 0, os.ErrClosed
	}
}

func (t *memTun) WritePacket(pkt []byte) error {
	t.deliver(pkt)
	return nil
}

func (t *memTun) Close() error {
	t.once.Do(func() {
		close(t.done)
		close(t.events)
	})
	return nil
}

// shorten keeps the useful tail of a log line for display.
func shorten(line string) string {
	if i := strings.LastIndex(line, ": "); i >= 0 && i+2 < len(line) {
		line = line[i+2:]
	}
	if len(line) > 80 {
		line = line[:80] + "…"
	}
	return line
}
