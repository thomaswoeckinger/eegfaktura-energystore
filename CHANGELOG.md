# Changelog

All notable changes to **eegfaktura-energystore (Go measurement/energy data store)** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and
versioning follows the deployment release tags. Detailed diffs stay in the `git log`;
this changelog highlights the changes relevant for overview and operations.

## [Unreleased]

### Changed
- CI: Preview-Deployments (ADR-0007) — Push auf `preview/**` baut+deployt on-demand in die Dev-Zone (sha-pinned, kein `:latest`), Auto-Reset bei Branch-Delete.

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
