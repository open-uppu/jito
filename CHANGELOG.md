# Changelog

All notable changes to jito are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and jito adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Versions are tagged by the `jito-rel` release engineer sub-agent and
published as GitHub Releases by PM (main session) after the CEO
human-gate approval. Drafts are uploaded with `--draft` so the release
URL exists but is invisible to the public until promotion.

---

## v0.2.0 — 2026-06-28

> **Loop 1 → 4 ship.** jito graduated from MVP to a sandboxed,
> checkpointable, customisable multi-agent CLI. Four parallel
> engineering loops merged into `main` over 24 hours.

### Features

- **JITO.md context loader** (`internal/context`, LOOP #1) — the
  first-class counterpart to `CLAUDE.md` / `GEMINI.md`. Walks the
  current directory and every ancestor, applies `.jitoignore`, and
  merges all matches into the LLM system prompt. Verbatim on
  re-read.
- **2-minute heartbeat package** (`internal/heartbeat`, LOOP #1) —
  race-safe, dependency-free, append-only daily log under
  `~/.jito/heartbeat/`. Sub-agents and the `jito heartbeat` CLI both
  use it. ≥ 95% test coverage.
- **Custom slash commands** (`internal/commands`, LOOP #2) — TOML
  manifests in `~/.jito/commands/*.toml`, BurntSushi/toml parser,
  POSIX-style `shlex` argument splitter, in-process registry
  consulted by `chat`, `run`, and `init`.
- **Permission picker** (`internal/permissions`, LOOP #2) — policy
  modes (`allow`, `ask`, `deny`, `audit`), per-session approval cache,
  and a Bubble Tea modal that surfaces an Allow / Always / Deny
  prompt when the policy is `ask`.
- **Checkpoint / resume** (`internal/session`, LOOP #3) — store
  every chat + run into SQLite (`internal/store`), with row-level
  audit log of every tool call, and the `jito resume <id>` and
  `jito sessions` CLI commands. Backed by OWASP-aware redaction of
  secrets in the audit trail.
- **BashTool sandbox hardening** (`internal/permissions/sandbox.go`
  + `internal/tools/bash_wrap.go`, LOOP #3) — path canonicalisation,
  env scrub, network egress block, dangerous-cmd denylist. 47 sandbox
  checks across 7 test classes, 0 bypass.
- **Loop Engineering wire-up** (`internal/loop`, LOOP #4) — strict
  regex-validated run-log appender, reads `STATE.md`, writes
  `run-log-YYYY-MM-DD.md` under
  `~/.openclaw/workspace/state/loop-engineering/`. The `jito loop
  state|run-log|status` CLI mirrors the same files for humans.
- **`jito spawn <agent> <task>`** (LOOP #4) — the entry point for
  the CEO-Profile Loop Engineering pipeline. Announces
  STARTED / DONE / BLOCKED to the run-log automatically; supports
  `--dry-run` for non-loop bookkeeping.
- **goreleaser pipeline** (`.goreleaser.yml`, LOOP #4) — 6-platform
  build matrix (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64,
  windows/amd64, windows/arm64) with tar.gz (Unix) + zip (Windows)
  archives. `--draft` releases only; PM publishes after CEO approval.
- **CHANGELOG.md** (this file, LOOP #4) — auto-generated from
  conventional commits by the goreleaser `changelog:` block; the
  `Other changes` group collapses docs / chore / test commits.

### Bug fixes

- The `c084e97` baseline build was broken in CI; LOOP #1's heartbeat
  package commit `57b4987` re-introduced a working `bin/jito`.
- LOOP #2's `permission picker` initially used the wrong key for
  `chat` lookups (typed the session id instead of the message id);
  fixed in `e50ead8`.
- LOOP #3's `cobra import unused` in `internal/cli/resume.go` caused
  a `go build` failure that was fixed in `1e18953` (the respawn-2
  commit).

### Other changes

- The two Sprint A + Sprint B commits (`202c31b`, `a949a1f`) are
  absorbed into v0.2.0 since the v0.1.0 tag was never pushed.
- README.md redesigned in LOOP #4 to match the v0.2.0 feature
  surface (8 sections — see `docs(readme)` commit).
- `agents/jito-rel.md` published as the release-engineer spec (15
  sections, version 0.1.0).
- `state/loop-engineering/run-log-2026-06-28.md` is the append-only
  heartbeat feed for jito v0.2.0's four sub-loops.

---

## v0.1.0 — 2026-06-27

> **MVP.** A multi-mode AI agent CLI wired to a single LLM provider
> (Minimax). Reached feature parity with `opencode` and `gemini-cli`
> in five modes (dev, reason, create, audit, universal) before any of
> the LOOP #1 → #4 work began.

### Features

- `b9503a6` — `jito v0.1.0 — MVP multi-mode AI agent CLI` — TUI
  (Bubble Tea), streaming responses, SQLite store, custom tools, mock
  provider, minimax API client.
- `202c31b` — Sprint A — TUI, streaming, SQLite, tools, mock provider.
- `a949a1f` — Sprint B+C — failover, themes, worktree, spawn, plan,
  MCP, telemetry, plugin, doctor, completion, update.
- `c084e97` — first commit (broken CI; superseded by `b9503a6`).

### Known gaps addressed in v0.2.0

- No `JITO.md` context loading — added in v0.2.0 / LOOP #1.
- No sub-agent lifecycle visibility — added in v0.2.0 / LOOP #4
  (`jito loop status`, run-log appender).
- No custom slash commands — added in v0.2.0 / LOOP #2.
- No permission prompt UI — added in v0.2.0 / LOOP #2.
- No checkpoint / resume — added in v0.2.0 / LOOP #3.
- No BashTool sandbox — added in v0.2.0 / LOOP #3.
- No cross-platform binaries — added in v0.2.0 / LOOP #4
  (goreleaser).

---

[Unreleased]: https://github.com/uppu/jito/compare/v0.2.0...HEAD
[v0.2.0]: https://github.com/uppu/jito/releases/tag/v0.2.0
[v0.1.0]: https://github.com/uppu/jito/releases/tag/v0.1.0
