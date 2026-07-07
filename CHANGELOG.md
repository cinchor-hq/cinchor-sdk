# Changelog

All notable changes to `@cinchor/sdk` are documented here. This project follows
[Semantic Versioning](https://semver.org) (0.x: minor = feature/breaking, patch = fix).

## 0.2.1 — 2026-07-07

### Changed
- **Relicensed to Apache-2.0** (from proprietary). The client SDK is now open
  source; the managed gateway and service remain proprietary. Apache-2.0 chosen
  for its explicit patent grant (this SDK does post-quantum signing).

## 0.2.0 — 2026-07-07

### Added
- `EnforcementCode.Unauthorized` (6): surfaced when the on-chain contract rejects
  an action whose caller is not the capability's bound agent (caller binding).

### Fixed
- **enforce no longer fabricates a deny on a settle timeout.** The verdict is now
  taken from committed pre-state; a slow settlement is reported as unsettled rather
  than as a refusal, so an action that later commits is never misreported as denied.
- **Absence is never a fast-deny.** A not-yet-visible capability (e.g. a mint still
  propagating) is treated as pending, not refused.
- Mint path funds the principal on issue, closing the case where a drained
  bootstrap principal made mints silently fail.

## 0.1.0 — 2026-06-27

- Initial public release: `enforce` / `attest`, capability registry, post-quantum
  (ML-DSA-44) signing, salted-context hashing with per-tenant crypto-shred.
