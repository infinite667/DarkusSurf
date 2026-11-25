# Third-party components and release obligations

- Go runtime and standard library: https://go.dev/ ; notices in `licenses/GO-LICENSE.txt` and `licenses/go-vendor/`.
- Tor Expert Bundle (Tor, GeoIP data and pluggable transports): https://www.torproject.org/download/tor/ ; upstream notices from the supplied bundle preserved in `licenses/tor-bundle/`. The original archive was not independently authenticated. Obtain the official signed release before building.
- Tor source: https://gitlab.torproject.org/tpo/core/tor ; Lyrebird: https://gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/lyrebird ; other upstream notices are in `licenses/tor-bundle/`. These links are reference locations, not a verified complete corresponding-source offer for the supplied binaries. Identify the exact upstream versions and all linked/bundled components, preserve required notices and provide corresponding source where required before redistributing.
- Logo/artwork supplied by maintainer; rights have not been independently verified. Confirm permission before public distribution.
- Firefox, Brave, Chrome, Edge and Chromium are external applications and are not included or affiliated with this project.

The repository intentionally omits the downloaded Windows bundle. `TOR-BUNDLE-SHA256.txt` is a historical file manifest, not evidence of upstream authenticity. The project license does not override third-party licenses. Confirm the proposed GPL-3.0 license and compatibility/redistribution obligations before release.
