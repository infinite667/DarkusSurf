# DarkusSurf 0.4.0 — Windows test checklist

Development preview. Keep tested v0.3.0 as fallback. Verify the original Tor bundle signature before running. Test on a noncritical network or VM first, especially Windows proxy mode. Note the original Windows proxy configuration before enabling it. Never treat an internal IP check as a leak test.

1. Launch EXE: native window opens; all nine pages fit and respond. Windows proxy and circuit refresh are off.
2. Connect Tor without bridges; verify the controller's Tor exit. Launch Brave via Applications and verify in that separate browser. Ordinary browser should remain direct when Windows proxy is off.
3. Traffic: counters populate and increase during browsing; rates update. Circuit page: built circuit entries appear; no stale values should be displayed after disconnect.
4. Circuit refresh: choose 3, 15, 30 seconds and toggle on; verify requests are never closer than 10 seconds, existing connections are not promised to change, and refresh stops on disconnect. Manually verify if desired. IP changes are not guaranteed.
5. Bridge: while disconnected, paste trusted vanilla or obfs4 bridge lines via clipboard. Check count, connect, verify exit, disconnect and clear. Invalid lines should be refused.
6. Windows proxy: confirm off; record current settings. Enable while connected and confirm prompt. Check Windows proxy settings and test a fresh proxy-aware ordinary browser. Verify IP there, not only in dashboard. Test restoration by Restore original Windows proxy, then repeat and Disconnect. Test Exit. Verify exact original settings after each. If testing crash recovery, terminate in a VM and relaunch to restore; check Windows Settings > Network & internet > Proxy if restoration fails. Apps that ignore the proxy can still connect directly. No firewall enforcement.
7. DNS, IPv6 and WebRTC: test independently in the selected browser and compatible system-proxied app. Do not label protection verified until testing passes. No system-wide leak protection.
8. Confirm previously tested tray, selection persistence, disconnect and native exit behavior remain intact.

Report observed results, failures, browser version and Windows version. Do not share private bridge addresses, real IP addresses, or browsing history in diagnostics.
