<p align="center">
  <img src="https://raw.githubusercontent.com/iknalos/WarpGo-Windows/main/docs/icon-256.png" width="96" alt="WarpGo icon">
</p>

<h1 align="center">WarpGo: free Cloudflare WARP VPN for Android</h1>

<p align="center">
  <b>WarpGo</b> is a free <b>Cloudflare WARP client for Android</b>, with its full source code here on GitHub.<br>
  One tap sends your phone's traffic through Cloudflare's <b>1.1.1.1 WARP</b> network over <b>WireGuard</b> or <b>MASQUE (HTTP/3)</b>.<br>
  Its <b>Auto</b> mode finds the tunnel that actually works on school, campus, office and hotel Wi-Fi that blocks VPNs.
</p>

<p align="center">
  <a href="https://github.com/iknalos/WarpGo/releases/latest/download/WarpGo.apk"><img src="https://img.shields.io/badge/Download-WarpGo.apk-F6821F?style=for-the-badge&logo=android&logoColor=white" alt="Download the WarpGo APK"></a>
</p>

<p align="center">
  <a href="https://github.com/iknalos/WarpGo/releases/latest"><img src="https://img.shields.io/github/v/release/iknalos/WarpGo?label=latest&color=F6821F" alt="Latest release"></a>
  <a href="https://github.com/iknalos/WarpGo/actions/workflows/build.yml"><img src="https://github.com/iknalos/WarpGo/actions/workflows/build.yml/badge.svg" alt="Build and device tests"></a>
  <img src="https://img.shields.io/badge/Android-7.0%2B-3DDC84?logo=android&logoColor=white" alt="Android 7.0 or newer">
  <a href="https://github.com/iknalos/WarpGo-Windows"><img src="https://img.shields.io/badge/also_on-Windows-0078D4?logo=windows&logoColor=white" alt="WarpGo for Windows"></a>
</p>

<p align="center">
  <b>Website:</b> <a href="https://iknalos.github.io/WarpGo/">iknalos.github.io/WarpGo</a> ·
  <b>Windows app:</b> <a href="https://github.com/iknalos/WarpGo-Windows">github.com/iknalos/WarpGo-Windows</a>
</p>

---

## Why WarpGo

Some networks let a VPN connect and then quietly drop all of its traffic. A
campus firewall might allow the WireGuard handshake and block every data packet
after it. Another network blocks UDP entirely. Either way, a VPN app can show
"Connected" while nothing loads.

WarpGo checks that **data really comes back** before it says Connected. When
one way of reaching Cloudflare is blocked, it tries the next one.

| Option | What it is | Gets past |
|---|---|---|
| **WireGuard** · port 4500 / 2408 / 500 | the classic WARP tunnel over UDP | networks that block one UDP port |
| **MASQUE** · HTTP/3 | WARP inside QUIC on UDP 443, which looks like normal web traffic | firewalls that recognise WireGuard and drop it on every port |
| **MASQUE** · HTTP/2 | the same over TCP 443 | networks that block UDP altogether |

## Features

- **One-tap Cloudflare WARP on Android.** All your phone's traffic goes through Cloudflare's WARP network.
- **Auto mode.** Tests WireGuard on three ports, then MASQUE over HTTP/3 and HTTP/2, and connects with the first that passes real data. It remembers what worked on each network.
- **Honest status.** Shows *Connected ✓* only after traffic actually comes back through the tunnel. If nothing arrives within 10 seconds, it tells you.
- **Test every option.** Checks all five ways without connecting. Useful for working out what a network blocks.
- **Free, no sign-up, no root.** WarpGo comes with a WARP account built in, so it works even on networks that block Cloudflare's sign-up page.
- **Updates itself.** Shows *Update available* when a new build is out, downloads it, checks its SHA-256 and installs it.
- **Small and clean.** A single APK with no ads, no analytics and no trackers.

## Download and install

1. Download **[WarpGo.apk](https://github.com/iknalos/WarpGo/releases/latest/download/WarpGo.apk)** from the [latest release](https://github.com/iknalos/WarpGo/releases/latest).
2. Open it and allow your browser or file manager to install apps, if Android asks.
3. Open WarpGo and tap **Connect**. Android asks for VPN permission the first time.

Requires Android 7.0 or newer on a 64-bit phone (arm64; x86_64 also works).
WarpGo isn't on Google Play. Releases are built and signed by
[GitHub Actions](.github/workflows/build.yml) from this repository.

> Builds older than 1.0.5 were signed with throwaway keys. If you have one of
> those, uninstall it once before installing the current version.

**On a PC?** [WarpGo for Windows](https://github.com/iknalos/WarpGo-Windows) is the same tunnel with the same Auto mode, as a Windows 10/11 installer.

## How Auto works

Auto tries each option one at a time:

1. whatever worked last time on this network,
2. WireGuard on 4500 (the option WarpGo has always used), then 2408, then 500,
3. MASQUE over HTTP/3, then HTTP/2.

Each test builds a complete tunnel **in memory** and sends a DNS query to `1.1.1.1`
through it. A test passes only if the answer comes back. That catches the
firewalls that accept the WireGuard handshake and then drop everything, which
would otherwise look like a working connection. Networks where WireGuard works
keep using WireGuard, and MASQUE is the fallback.

**Test every option** runs all the tests without connecting and lists the results.

## FAQ

**Is WarpGo free?**
Yes. WarpGo is free, its source code is in this repository, and it uses Cloudflare's free WARP service.

**Is this the official Cloudflare 1.1.1.1 app?**
No. WarpGo is an independent, unofficial client. Cloudflare's own app is
[1.1.1.1 + WARP](https://one.one.one.one/). WarpGo connects to the same WARP network,
and adds Auto mode and the real-data tests.

**Does WarpGo work on school or campus Wi-Fi?**
It was built for that. It was first written for a campus network that blocked
gaming traffic, and later used on one that dropped WireGuard data on every port.
MASQUE over HTTP/3 got through there. Use **Test every option** to see what your
network allows. Follow your network's rules.

**Does it hide my IP address or change my country?**
Websites see a Cloudflare IP instead of your network's, usually in or near your own
country. WARP is designed for privacy and reliability, not for appearing to be somewhere else.

**Does WarpGo collect data?**
No. It connects only to Cloudflare (WARP servers and the sign-up API) and to GitHub
(update check). It has no analytics.

**Does it need root?**
No. It uses Android's standard VPN API (`VpnService`).

**Can I choose the protocol myself?**
Yes. Pick WireGuard 4500, 2408 or 500, MASQUE HTTP/3 or MASQUE HTTP/2 instead of Auto.

## How it works

- `core/` is the tunnel engine in Go, bound into the app with `gomobile`:
  WireGuard via [wireguard-go](https://git.zx2c4.com/wireguard-go), MASQUE via
  [usque](https://github.com/Diniboy1123/usque) (MIT).
- WARP accounts (one WireGuard, one MASQUE) are registered ahead of time and
  injected at build time from GitHub Actions secrets, because some campus
  networks intercept WARP's sign-up API. Local builds without the secrets register
  their own accounts on the phone.
- WarpGo's own connections (the tunnel itself, tests, update checks) bypass the
  VPN, so the tunnel never loops into itself.

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

Or install [WarpGo for Windows](https://github.com/iknalos/WarpGo-Windows) and run `WarpGo.exe --test`.

---

*WarpGo is not affiliated with, endorsed by or sponsored by Cloudflare, Inc.
"Cloudflare" and "WARP" are trademarks of Cloudflare, Inc. WireGuard is a registered
trademark of Jason A. Donenfeld.*
