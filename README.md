<p align="center"><img src="ui/logo.png" width="140" alt="DarkusSurf logo"></p>
<h1 align="center">DarkusSurf</h1>
<p align="center"><strong>Privacy. Control. Anonymity.</strong></p>
<p align="center">Independent Tor connection manager and network privacy toolkit</p>

![Version](https://img.shields.io/badge/version-v0.4.0-8b5cf6) ![Status](https://img.shields.io/badge/status-development%20preview-orange) ![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux-30303a)

## Overview

DarkusSurf is an independent Go application for managing Tor connections, selectively launching supported browsers with proxy-configured profiles, and inspecting connection health. It is intended for personal privacy, education and authorized security research. It is not affiliated with the Tor Project or Parrot Security. It is not a VPN, firewall or audited anonymity product.

**Development preview:** Do not rely on this application for sensitive anonymity needs. A verified Tor exit only confirms the specific request made through Tor, not protection of every application. For anonymity-sensitive browsing, use the [official Tor Browser](https://www.torproject.org/download/).

## Features

| Feature | v0.4.0 scope |
| --- | --- |
| Native dark-purple Windows GUI | Dashboard, tray, settings, diagnostics; no external browser for the interface |
| Tor connection management | Connect/disconnect, bootstrap, exit verification, optional bounded reconnection |
| Selective browser routing | Firefox, Chrome, Edge, Brave, Chromium; separate proxy-configured profiles |
| Windows proxy mode | Explicit opt-in for proxy-aware HTTP/HTTPS applications; disabled by default |
| Network restoration | Recovery journal and best-effort restoration of previous user proxy settings |
| DNS / IPv6 controls | Proxy and managed-profile controls only; no OS-wide enforcement |
| Traffic monitor | Tor-process byte totals and sampled upload/download rates |
| Circuit viewer | Built general-purpose Tor circuits; not a site-specific path |
| Circuit refresh | Off by default; requested interval 3–30 seconds, actual minimum 10 seconds and Tor rate limits; no guaranteed IP change |
| Bridges | Import trusted vanilla/obfs4 bridge lines in Windows; session-only |
| Local settings | Persistent application preferences; no application telemetry |
| Linux | Shared backend and existing local web GUI; system Tor or separately supplied verified bundle |

## Screens and usage

The Windows interface includes Dashboard, Applications, Network, Traffic, Circuits, Bridges, Diagnostics, Settings and About. See the [user guide](docs/USER-GUIDE.md) for detailed behavior. No screenshots are included until genuine v0.4.0 captures are available.

1. Obtain a verified build from the repository's Releases section when available, or build locally as described below.
2. Start DarkusSurf, connect to Tor and wait for bootstrap.
3. In Applications, select a supported browser, save selection and launch it via Tor.
4. Verify the connection **inside the newly launched window** at [check.torproject.org](https://check.torproject.org/). Existing ordinary windows are not automatically routed.
5. Close the routed windows before disconnecting or exiting. If you opted into Windows proxy mode, verify that your prior settings were restored.

## Build from source

Go 1.23 or newer (use a currently supported security-maintained Go version). No third-party Go modules are required.

**Windows x64:** Download and authenticate the official [Tor Expert Bundle](https://www.torproject.org/download/tor/) and extract it following [vendor instructions](vendor/tor/README.md). Run `Build-Windows.cmd` from the repository root. Output: `dist/DarkusSurf.exe`. The build embeds the supplied Tor bundle; the repository intentionally excludes downloaded executables. The included `.syso` is a generated Windows icon resource; regenerate with `python scripts/make-icon-resource.py` (requires clang).

**Linux:** Install Go, Tor and xdg-utils from trusted sources. Run `chmod +x Build-Linux.sh Launch-Linux.sh scripts/Build-Linux.sh` and `./Build-Linux.sh`. Output: `dist/DarkusSurf`. Linux uses the older local web interface, and not all Windows controls are exposed there.

Run `go test -race ./...` and `go vet ./...` on Linux; see [build report](BUILD-REPORT.txt) and [Windows checklist](WINDOWS-TEST-CHECKLIST.md). Offline tests use simulated Tor, not real leak or anonymity testing. The previous v0.3.0 Windows browser-routing workflow was user-tested; new v0.4.0 Windows networking features still require live validation.

## Privacy and limitations

- Tor does not carry arbitrary UDP traffic. Windows proxy mode only affects compatible clients. True system-wide routing, an OS-level kill switch and OS-wide DNS/IPv6 leak prevention are not implemented.
- Regular browsers do not provide the same anti-fingerprinting protections as Tor Browser. Browser background traffic, WebRTC and other components can bypass proxy configuration. Independent leak tests remain pending.
- Circuit refresh requests affect future streams; a new exit address is not guaranteed. Existing streams may remain active.
- DarkusSurf stores local preferences and, when proxy mode is enabled, a recovery journal. It does not intentionally collect browsing history or send application telemetry. Verification makes an explicit external request to the Tor Project. Temporary files are not securely erased; crashes can leave state behind.
- If proxy restoration fails, use Windows Settings > Network & internet > Proxy to restore your previous settings. See the guide. Do not delete the recovery journal until recovery is complete.

## Responsible use and security

Use DarkusSurf on devices and networks where you have permission, in compliance with applicable laws and policies. Do not use or distribute it to facilitate unauthorized access, harassment, fraud, malware campaigns or other harmful conduct. Do not post credentials, private bridge details, personal IP addresses or sensitive logs in issues. See [SECURITY.md](SECURITY.md) for vulnerability reporting and [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

## Licensing and credits

The repository contains a proposed GPL-3.0 license for original DarkusSurf code; the maintainer must confirm licensing and artwork rights before public publication. Third-party components retain their own licenses. See [THIRD-PARTY.md](THIRD-PARTY.md) and the `licenses/` directory. The Tor Project and upstream component contributors retain their attribution and rights. No affiliation or endorsement is claimed.
