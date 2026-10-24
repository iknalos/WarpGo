# WarpGo

A small Android app that tunnels your phone's traffic through **Cloudflare WARP**,
for networks that interfere with traffic (e.g. campus Wi-Fi blocking games).

It speaks both of WARP's protocols:

| Option | What it is | Gets past |
|---|---|---|
| WireGuard · 4500 / 2408 / 500 | the classic WARP tunnel over UDP | networks that block one UDP port |
| MASQUE · HTTP/3 | WARP inside QUIC on UDP 443, what Cloudflare's own app uses now | firewalls that recognise and drop WireGuard on every port |
| MASQUE · HTTP/2 | the same over TCP 443 | networks that block UDP |

## Auto

**Auto** (the default) tests each option one at a time and connects with the first
that really passes data. Each test runs a whole tunnel in memory and waits for a
DNS answer to come back through it, so a firewall that answers the WireGuard
handshake and then drops everything (UC-SECURE does exactly this) shows up as a
failure instead of a fake "Connected ✓". WireGuard on 4500 is tried first, so
networks where WarpGo already worked keep using WireGuard; MASQUE is the fallback.
Auto remembers what worked on each network and tries that first next time.

**Test every option** runs all the tests without connecting and lists the results.

After connecting, the status only says **Connected ✓** once data has actually come
back through the tunnel; if it hasn't after 10 seconds, WarpGo says so.

## How it works

- `core/` is the tunnel engine in Go, bound into the app with `gomobile`:
  WireGuard via [wireguard-go](https://git.zx2c4.com/wireguard-go), MASQUE via
  [usque](https://github.com/Diniboy1123/usque) (MIT).
- WARP accounts (one WireGuard, one MASQUE) are registered ahead of time and
  injected at build time from GitHub Actions secrets, because some campus
  networks intercept WARP's signup API. Local builds without the secrets register
  their own accounts on the phone.
- WarpGo's own connections (the tunnel itself, tests, update checks) bypass the
  VPN so the tunnel never loops into itself.

## Install and updates

Install `WarpGo.apk` from the [latest release](../../releases/latest). Android asks
for VPN permission the first time you connect. After that WarpGo updates itself:
it shows **Update available** when a newer build is out.

Builds from before 1.0.5 were signed with throwaway keys, so uninstall an old
WarpGo once before installing this one.

## Build

Every push to `main` runs `.github/workflows/build.yml`:

1. **core**: `go vet`, `go test`, then `gomobile bind` → `warpcore.aar`
2. **build**: signed release APK (signer checked against `signing-cert.sha256`)
3. **device-test**: on an Android 14 emulator, every option must pass data, and
   both a WireGuard and a MASQUE tunnel must carry the device's own traffic
4. **release**: `WarpGo.apk` + `latest.json` for the in-app updater

To check a network from a PC with the same tests the app runs:

```
cd core
go run ./cmd/probe -wg wgcf-profile.conf -masque masque-account.json
```
