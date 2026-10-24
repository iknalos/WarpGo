// Package warpcore is WarpGo's tunnel engine, bound into the Android app with
// gomobile. It speaks both protocols Cloudflare WARP offers:
//
//   - WireGuard (wireguard-go), the classic WARP tunnel on UDP 2408/4500/500/...
//   - MASQUE (usque), HTTP/3 CONNECT-IP on UDP 443, with an HTTP/2 TCP 443 fallback
//
// Some campus firewalls (UC-SECURE's FortiGate, 2026-10-23) let the WireGuard
// handshake through and then drop every data packet, on every port. MASQUE
// looks like ordinary HTTPS to them and passes.
package warpcore

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Diniboy1123/usque/api"
)

const (
	// Cloudflare's WARP WireGuard peer key, the same for every account.
	wgPeerKey   = "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo="
	wgHost      = "engage.cloudflareclient.com"
	wgFallback  = "162.159.192.1"
	clientV4    = "172.16.0.2"
	masqueSNI   = "consumer-masque.cloudflareclient.com"
	masqueH2Def = "162.159.198.2"
	tunnelMTU   = 1280

	// Registration API constants verified against wgcf v2.2.31 (2026-06-23).
	regAPI           = "https://api.cloudflareclient.com/v0a1922"
	regClientVersion = "a-6.3-1922"
	regUserAgent     = "okhttp/3.12.1"
)

// WgAccount is a WARP device registered with a WireGuard key.
type WgAccount struct {
	PrivateKey string `json:"private_key"` // base64 curve25519
	AddressV4  string `json:"address_v4,omitempty"`
	AddressV6  string `json:"address_v6,omitempty"`
}

// MasqueAccount mirrors usque's config.json, minus the API id/token that the
// tunnel itself never needs.
type MasqueAccount struct {
	PrivateKey     string `json:"private_key"` // base64 DER, EC P-256
	EndpointV4     string `json:"endpoint_v4"`
	EndpointV6     string `json:"endpoint_v6,omitempty"`
	EndpointH2V4   string `json:"endpoint_h2_v4,omitempty"`
	EndpointPubKey string `json:"endpoint_pub_key"` // PEM, pinned
	IPv4           string `json:"ipv4"`
	IPv6           string `json:"ipv6,omitempty"`
}

func parseWg(s string) (*WgAccount, error) {
	var a WgAccount
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return nil, fmt.Errorf("bad WireGuard account: %w", err)
	}
	if a.PrivateKey == "" {
		return nil, errors.New("WireGuard account has no private key")
	}
	if a.AddressV4 == "" {
		a.AddressV4 = clientV4
	}
	a.AddressV4 = stripPrefix(a.AddressV4)
	a.AddressV6 = stripPrefix(a.AddressV6)
	return &a, nil
}

func parseMasque(s string) (*MasqueAccount, error) {
	var a MasqueAccount
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return nil, fmt.Errorf("bad MASQUE account: %w", err)
	}
	if a.PrivateKey == "" || a.EndpointV4 == "" || a.EndpointPubKey == "" {
		return nil, errors.New("MASQUE account is incomplete")
	}
	if a.EndpointH2V4 == "" {
		a.EndpointH2V4 = masqueH2Def
	}
	if a.IPv4 == "" {
		a.IPv4 = clientV4
	}
	a.IPv4 = stripPrefix(a.IPv4)
	a.IPv6 = stripPrefix(a.IPv6)
	return &a, nil
}

// stripPrefix turns "2606:4700::1/128" into "2606:4700::1".
func stripPrefix(addr string) string {
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		return addr[:i]
	}
	return strings.TrimSpace(addr)
}

// RegisterWireGuard creates a fresh free WARP account with a new WireGuard key
// and returns it as WgAccount JSON. Fails on networks that intercept
// api.cloudflareclient.com; the app then falls back to the baked account.
func RegisterWireGuard() (string, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	pub := base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes())
	body := map[string]string{
		"key":        pub,
		"install_id": "",
		"fcm_token":  "",
		"tos":        time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"model":      "PC",
		"type":       "Android",
		"locale":     "en_US",
	}
	var reg struct {
		ID     string `json:"id"`
		Token  string `json:"token"`
		Config struct {
			Interface struct {
				Addresses struct {
					V4 string `json:"v4"`
					V6 string `json:"v6"`
				} `json:"addresses"`
			} `json:"interface"`
		} `json:"config"`
	}
	if err := regCall("POST", regAPI+"/reg", "", body, &reg); err != nil {
		return "", err
	}
	// Without warp_enabled the tunnel comes up but routes nothing.
	if err := regCall("PATCH", regAPI+"/reg/"+reg.ID, reg.Token, map[string]bool{"warp_enabled": true}, nil); err != nil {
		return "", err
	}
	out, err := json.Marshal(WgAccount{
		PrivateKey: base64.StdEncoding.EncodeToString(priv.Bytes()),
		AddressV4:  reg.Config.Interface.Addresses.V4,
		AddressV6:  reg.Config.Interface.Addresses.V6,
	})
	return string(out), err
}

func regCall(method, url, token string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", regUserAgent)
	req.Header.Set("CF-Client-Version", regClientVersion)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("WARP API %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// RegisterMasque creates a fresh free WARP account, enrolls a new P-256 key
// for MASQUE and returns it as MasqueAccount JSON.
func RegisterMasque() (string, error) {
	acct, err := api.Register("PC", "en_US", "", true)
	if err != nil {
		return "", fmt.Errorf("register: %w", err)
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", err
	}
	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", err
	}
	enrolled, err := api.EnrollKey(acct.ID, acct.Token, pubDER, "")
	if err != nil {
		return "", fmt.Errorf("enroll MASQUE key: %w", err)
	}
	if len(enrolled.Config.Peers) == 0 {
		return "", errors.New("enroll: no MASQUE peer in response")
	}
	peer := enrolled.Config.Peers[0]
	out, err := json.Marshal(MasqueAccount{
		PrivateKey:     base64.StdEncoding.EncodeToString(privDER),
		EndpointV4:     hostOnly(peer.Endpoint.V4),
		EndpointV6:     hostOnly(peer.Endpoint.V6),
		EndpointH2V4:   masqueH2Def,
		EndpointPubKey: peer.PublicKey,
		IPv4:           enrolled.Config.Interface.Addresses.V4,
		IPv6:           enrolled.Config.Interface.Addresses.V6,
	})
	return string(out), err
}

// hostOnly turns "162.159.198.1:0" or "[2606:4700:103::]:0" into the bare IP.
func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}

// masqueTLS builds the mutual-TLS config: our enrolled key with a throwaway
// self-signed cert, and the endpoint's key pinned from the account.
func masqueTLS(a *MasqueAccount) (*tls.Config, error) {
	der, err := base64.StdEncoding.DecodeString(a.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("MASQUE private key: %w", err)
	}
	priv, err := x509.ParseECPrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("MASQUE private key: %w", err)
	}
	block, _ := pem.Decode([]byte(a.EndpointPubKey))
	if block == nil {
		return nil, errors.New("MASQUE endpoint key is not PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("MASQUE endpoint key: %w", err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("MASQUE endpoint key is not ECDSA")
	}
	cert, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(0),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}, &x509.Certificate{}, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	return api.PrepareTlsConfig(priv, ecPub, [][]byte{cert}, masqueSNI, false)
}
