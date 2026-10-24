package warpcore

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestDNSQueryIsValidIPv4(t *testing.T) {
	q := dnsQuery(netip.MustParseAddr("172.16.0.2"), 0x1234, 41234, "www.cloudflare.com")
	if ipChecksum(q[:20]) != 0 {
		t.Fatalf("IPv4 header checksum does not verify")
	}
	if got := int(binary.BigEndian.Uint16(q[2:])); got != len(q) {
		t.Fatalf("total length %d, packet is %d bytes", got, len(q))
	}
	if q[9] != 17 || [4]byte(q[16:20]) != dnsServerV4 {
		t.Fatalf("not UDP to 1.1.1.1")
	}
}

func TestIsDNSAnswer(t *testing.T) {
	q := dnsQuery(netip.MustParseAddr("172.16.0.2"), 0xbeef, 45000, "www.cloudflare.com")
	// Turn the query into a reply: swap addresses and ports, set QR, ANCOUNT=1.
	r := append([]byte(nil), q...)
	copy(r[12:16], q[16:20])
	copy(r[16:20], q[12:16])
	copy(r[20:22], q[22:24])
	copy(r[22:24], q[20:22])
	r[28+2] |= 0x80
	binary.BigEndian.PutUint16(r[28+6:], 1)
	if !isDNSAnswer(r, 0xbeef, 45000) {
		t.Fatal("reply not recognised")
	}
	if isDNSAnswer(r, 0xbeee, 45000) || isDNSAnswer(r, 0xbeef, 45001) || isDNSAnswer(q, 0xbeef, 45000) {
		t.Fatal("wrong id, port or the query itself accepted as an answer")
	}
	if isDNSAnswer(r[:30], 0xbeef, 45000) {
		t.Fatal("truncated packet accepted")
	}
}
