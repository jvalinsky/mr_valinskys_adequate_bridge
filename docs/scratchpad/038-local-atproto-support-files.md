# Local ATProto Support Files

Date: 2026-04-01

Scope:
- local deployment and bootstrap files that support the post-Indigo ATProto stack
- repository notes captured alongside those support files

Notes:
- Added a checked-in `bridge.nix` host module for the bridge service instead of keeping the deployment fragment only as an ignored local file.
- Added a local Caddy proxy file plus Docker Compose wiring so the local ATProto stack can expose `pds.test` over HTTP/HTTPS and seed the relay host row explicitly.
- Updated the local bootstrap script to export a PLC URL alongside the host and relay values.
- Relaxed the flake Go build to use module mode instead of a stale vendored hash while the repo no longer carries a checked-in `vendor/` tree.
- Captured the SSB/SIP compliance review as a dated document and staged a small blob-ref parsing regression test.

Open questions:
- The compliance review is reference material, not an implemented fix list yet.
- The new blob-ref parsing test should be covered by a broader `go test ./...` pass when the next code-bearing commit goes out.
