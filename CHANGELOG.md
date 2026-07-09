# Changelog

All notable changes to **eegfaktura-energystore (Go measurement/energy data store)** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and
versioning follows the deployment release tags. Detailed diffs stay in the `git log`;
this changelog highlights the changes relevant for overview and operations.

## [Unreleased]

### Added
- Ops endpoint `POST /eeg/v2/{ecid}/rawdata/delete` to remove the raw energy data of a **single
  metering point** within a time range (maintenance for mis-assigned/duplicate data). Because one
  BadgerDB row (15-min timestamp) packs all metering points of the EC into shared arrays, deletion
  zeros only the target metering point's slot block (Consumers/Producers + QoV, resolved via the
  same `GetMetaInfo`/`cpmeta/0` mapping the read path uses) — co-located metering points in the
  same row stay untouched (core-correctness test in `store/deleteRawData_test.go`). Same iteration
  for `dryRun` (preview: affected timesteps + summed kWh, no write) and execute; batched and
  idempotent (re-zeroing is a no-op). Behind `ProtectApp` — cross-tenant deletion requires the
  `superuser` realm role; each execute writes one structured log line
  (operator/tenant/ec/zp/range/timesteps). Deletion is irreversible (value 0 / QoV 0); a later EDA
  re-import repopulates the slots.

## [1.0.3] – 2026-07-06

### Changed
- CI: Preview-Deployments (ADR-0007) — Push auf `preview/**` baut+deployt on-demand in die Dev-Zone (sha-pinned, kein `:latest`), Auto-Reset bei Branch-Delete.

### Fixed
- Raw-data query returned each timestamp multiple times for a re-registered
  metering point. When a metering point was deregistered from one member and
  re-registered under another, the old participant row remained with an
  overlapping active window, so the caller's `cps` list contained that metering
  point more than once for queries overlapping that window. The store holds
  exactly one data series per metering-point name, so each duplicate `cps` entry
  produced an identical repeated series ("each timestamp 4×"; support cases
  RC101586, RC105720). `QueryRawData` now de-duplicates the target list by
  metering-point name before querying — covering both raw endpoints
  (`/query/rawdata` and `/eeg/v2/{ecid}/raw`) and preventing the `Aggregate`
  function from double-counting. Single-metering-point queries and the Excel
  export were unaffected and remain unchanged.

## [1.0.2] – 2026-06-30

### Changed
- Hardening: close idle per-tenant Badger DBs after 15s instead of 60s; raise the keycloak token HTTP client timeout from 1s to 10s. (#16)

## [1.0.1] – 2026-06-30

### Fixed
- OOM / node-level SystemOOM under broad multi-tenant load: cap the per-tenant Badger block cache at 64 MB and the index cache at 16 MB (was the 256 MB default). (#15)

## [1.0.0] – 2026-06-28

First production release built entirely from public source.

### Changed
- CI: push to the registry's development tier with an auto-rollout bridge
  (dispatch-deploy, ADR-0005). (#7)
- Added AGPL-3.0 license; README with service overview and tech stack. (#2, #8)
