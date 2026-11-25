# Tor dependency (not checked into Git)

For a Windows x64 build, obtain the stable Windows x86_64 Tor Expert Bundle from https://www.torproject.org/download/tor/ and its corresponding signature. Verify the archive with the official Tor Project signing key using its verification guidance before extracting. Verify the fingerprint against the official documentation; do not trust a key solely because it is delivered with a download.

Extract the original `data`, `docs`, and `tor` directories directly into `vendor/tor/windows/` (so `vendor/tor/windows/tor/tor.exe` exists). The Windows build embeds this directory. Do not rename or remove upstream files. The historical bundle supplied for v0.4.0 was not independently signature-verified; `TOR-BUNDLE-SHA256.txt` records its file hashes, not authentication. Compare those hashes against a verified archive if reproducing the historical binary. If they differ, rebuild and issue a separately identified release candidate with new checksums and test results.

For Linux, use a trusted distro Tor package or a verified Linux expert bundle as described in the user guide. Do not commit downloaded bundles, private bridge lines or generated state. Before distributing binaries, satisfy all upstream license and corresponding-source requirements. See `../../THIRD-PARTY.md`.
