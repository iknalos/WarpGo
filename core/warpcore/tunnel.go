package warpcore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Diniboy1123/usque/api"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

type session struct {
	mode    string
	tun     *fdTun
	wg      *device.Device     // WireGuard sessions
	cancel  context.CancelFunc // MASQUE sessions
	started time.Time
}

var (
	sessMu sync.Mutex
	sess   *session
)

// StartWireGuard runs a WireGuard tunnel on the VpnService fd (Go takes
// ownership of fd and closes it on Stop).
func StartWireGuard(fd int, accountJSON string, port int) error {
	acct, err := parseWg(accountJSON)
	if err != nil {
		closeFd(fd)
		return err
	}
	t, err := newFdTun(fd)
	if err != nil {
		closeFd(fd)
		return err
	}
	dev, err := newWgDevice(t, acct, port, "tunnel")
	if err != nil {
		return err // newWgDevice closed t
	}
	install(&session{mode: fmt.Sprintf("wireguard-%d", port), tun: t, wg: dev, started: time.Now()})
	return nil
}

// StartMasque runs a MASQUE tunnel on the VpnService fd: HTTP/3 over UDP 443,
// or HTTP/2 over TCP 443 when http2 is set. Go takes ownership of fd.
func StartMasque(fd int, accountJSON string, http2 bool) error {
	acct, err := parseMasque(accountJSON)
	if err != nil {
		closeFd(fd)
		return err
	}
	cfg, err := masqueConfig(acct, http2)
	if err != nil {
		closeFd(fd)
		return err
	}
	t, err := newFdTun(fd)
	if err != nil {
		closeFd(fd)
		return err
	}
	cfg.Device = t
	ctx, cancel := context.WithCancel(context.Background())
	masqueUp.Store(false)
	go api.MaintainTunnel(ctx, cfg)
	install(&session{mode: masqueMode(http2), tun: t, cancel: cancel, started: time.Now()})
	return nil
}

// Stop tears down whichever tunnel is running.
func Stop() {
	sessMu.Lock()
	s := sess
	sess = nil
	sessMu.Unlock()
	stopSession(s)
}

func install(s *session) {
	sessMu.Lock()
	old := sess
	sess = s
	sessMu.Unlock()
	stopSession(old)
	log.Printf("tunnel started: %s", s.mode)
}

func stopSession(s *session) {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.wg != nil {
		s.wg.Close() // also closes the tun
	} else {
		_ = s.tun.Close()
	}
	masqueUp.Store(false)
	log.Printf("tunnel stopped: %s", s.mode)
}

// Stats reports the running tunnel as JSON: mode, whether the handshake /
// CONNECT-IP session is up, and bytes received (rx) and sent (tx).
func Stats() string {
	sessMu.Lock()
	s := sess
	sessMu.Unlock()
	out := map[string]any{"running": false}
	if s != nil {
		connected := masqueUp.Load()
		if s.wg != nil {
			connected = wgHandshakeDone(s.wg)
		}
		out = map[string]any{
			"running":   true,
			"mode":      s.mode,
			"connected": connected,
			"rx":        s.tun.rx.Load(),
			"tx":        s.tun.tx.Load(),
			"seconds":   int(time.Since(s.started).Seconds()),
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func masqueMode(http2 bool) string {
	if http2 {
		return "masque-h2"
	}
	return "masque-h3"
}

func masqueConfig(acct *MasqueAccount, http2 bool) (api.MaintainTunnelConfig, error) {
	tlsCfg, err := masqueTLS(acct)
	if err != nil {
		return api.MaintainTunnelConfig{}, err
	}
	var endpoint net.Addr = &net.UDPAddr{IP: net.ParseIP(acct.EndpointV4), Port: 443}
	if http2 {
		endpoint = &net.TCPAddr{IP: net.ParseIP(acct.EndpointH2V4), Port: 443}
	}
	return api.MaintainTunnelConfig{
		TLSConfig:       tlsCfg,
		KeepalivePeriod: 30 * time.Second,
		Endpoint:        endpoint,
		MTU:             tunnelMTU,
		ReconnectDelay:  time.Second,
		AlwaysReconnect: true,
		UseHTTP2:        http2,
	}, nil
}

func newWgDevice(t tun.Device, acct *WgAccount, port int, label string) (*device.Device, error) {
	priv, err := keyHex(acct.PrivateKey)
	if err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("WireGuard private key: %w", err)
	}
	peer, _ := keyHex(wgPeerKey)
	endpoint := wgEndpoint(port)
	logger := &device.Logger{
		Verbosef: func(format string, args ...any) {
			if strings.Contains(format, "handshake initiation") || strings.Contains(format, "handshake response") || strings.Contains(format, "Handshake did not complete") {
				log.Printf("wg "+label+": "+format, args...)
			}
		},
		Errorf: func(format string, args ...any) { log.Printf("wg "+label+" error: "+format, args...) },
	}
	dev := device.NewDevice(t, conn.NewDefaultBind(), logger)
	uapi := "private_key=" + priv + "\n" +
		"replace_peers=true\n" +
		"public_key=" + peer + "\n" +
		"endpoint=" + endpoint + "\n" +
		"persistent_keepalive_interval=25\n" +
		"replace_allowed_ips=true\n" +
		"allowed_ip=0.0.0.0/0\n" +
		"allowed_ip=::/0\n"
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, err
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, err
	}
	log.Printf("wg %s: endpoint %s", label, endpoint)
	return dev, nil
}

// wgEndpoint resolves engage.cloudflareclient.com (IPv4) and falls back to its
// well-known address if DNS is slow or blocked.
func wgEndpoint(port int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ip := wgFallback
	if ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", wgHost); err == nil && len(ips) > 0 {
		ip = ips[0].String()
	}
	return net.JoinHostPort(ip, fmt.Sprint(port))
}

func wgHandshakeDone(dev *device.Device) bool {
	state, err := dev.IpcGet()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(state, "\n") {
		if v, ok := strings.CutPrefix(line, "last_handshake_time_sec="); ok {
			return v != "0"
		}
	}
	return false
}

func keyHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", err
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("key is %d bytes, want 32", len(raw))
	}
	return hex.EncodeToString(raw), nil
}

func closeFd(fd int) { _ = os.NewFile(uintptr(fd), "tun").Close() }
