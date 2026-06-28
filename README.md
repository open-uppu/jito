# jito ⚡

> Multi-mode AI agent CLI for the **open-uppu Enterprise IT Master
> Blueprint** — sandboxed, checkpointable, context-aware, multi-agent.
> Powered by Minimax-M3 (OpenAI-compatible API).

<div align="center">

```
      _ ____   ___
     | (_) |__(_)
  ___| | | '_ \ | |
 / _ \ | | |_) || |
|  __/ | |_.__/_| |
 \___|_|         |_|
```

**v0.2.0** · `Minimax-M3` · Go 1.25 · Coverage ≥ 90% per pkg · License: Private (uppu internal)

</div>

---

## 1 · Hero

| What | Why |
|---|---|
| 5 first-class modes (`dev`, `reason`, `create`, `audit`, `universal`) | Pick the right persona without rewriting the system prompt. |
| `JITO.md` context loader | Hierarchical project memory, exactly like `CLAUDE.md` / `GEMINI.md`. |
| Custom slash commands (TOML) | `~/.jito/commands/*.toml` — define your own `/review`, `/pr`, `/release`. |
| Permission picker (Bubble Tea) | `ask` mode surfaces an Allow / Always / Deny modal for every Bash call. |
| BashTool sandbox | Path canonicalisation, env scrub, network block, dangerous-cmd denylist. |
| Checkpoint / resume | Every chat + run is in SQLite. `jito resume <id>` brings it back. |
| Multi-agent spawn | `jito spawn <agent> <task>` announces every lifecycle to the loop-engineering log. |
| Cross-platform binaries | linux/amd64 · linux/arm64 · darwin/amd64 · darwin/arm64 · windows/amd64 · windows/arm64 |

---

## 2 · Demo showcase

```
$ jito --mode=audit run "review this PR"
[jito] context: 2 files loaded (JITO.md + .jito/commands/review.toml)
[jito] mode:    audit
[jito] tools:   bash (sandboxed), read, write, edit
[jito] model:   minimax/MiniMax-M3
✅ /review started: 12 files changed, +347/-89
   ▸ security    — no issues
   ▸ style       — 2 nitpicks
   ▸ tests       — coverage +0.4% (89.2% → 89.6%)
   ▸ deps        — 1 outdated (pflag 1.0.5 → 1.0.6)
🟢 DONE in 6.4s  →  audit-report.md written

$ jito chat
   ┌─ jito ⚡  chat ─────────────────────────────────┐
   │ [dev]  > refactor internal/session/checkpoint  │
   │        > into a streaming pipeline             │
   │        ▌                                       │
   │                                                  │
   │  > /review                                      │
   │  ┌────────────────────────────────────────────┐ │
   │  │ ✅  Allow   Always   Deny                  │ │
   │  └────────────────────────────────────────────┘ │
   └──────────────────────────────────────────────────┘

$ jito loop status
state_dir: ~/.openclaw/workspace/state/loop-engineering
loops:     4
blockers:  0
entries:   17
last:      [DONE ship-ready] jito-rel-LOOP4 — all metrics
```

---

## 3 · Why jito?

| Capability | **jito** v0.2.0 | `opencode` | `gemini-cli` |
|---|:---:|:---:|:---:|
| 5 mode personas (`dev` / `reason` / `create` / `audit` / `universal`) | ✅ | ❌ (single mode) | ❌ (single mode) |
| `JITO.md` hierarchical context | ✅ | ❌ | ⚠️ (`GEMINI.md` only) |
| Custom slash commands (TOML) | ✅ | ⚠️ (JS hooks) | ❌ |
| Permission picker (Allow / Always / Deny modal) | ✅ | ❌ | ❌ |
| BashTool sandbox (path / env / network) | ✅ | ❌ | ⚠️ (basic) |
| Checkpoint / resume (SQLite) | ✅ | ❌ | ❌ |
| Multi-agent spawn + lifecycle log | ✅ | ❌ | ❌ |
| Cross-platform binaries (6 targets) | ✅ | ⚠️ (3) | ⚠️ (4) |
| OpenAI-compatible provider | ✅ | ✅ | ❌ (Gemini only) |
| Audit log with redaction | ✅ | ❌ | ❌ |

---

## 4 · Quickstart

Five commands from zero to a useful answer.

```bash
# 1. Build (or download a release from GitHub)
go install github.com/uppu/jito/cmd/jito@latest
#    or, from a cloned repo:
#    go build -o bin/jito ./cmd/jito

# 2. Initialise config
jito init

# 3. Point at a provider
export JITO_API_KEY=sk-your-minimax-key
export JITO_BASE_URL=https://api.minimax.io/v1   # default

# 4. (Optional) Drop a JITO.md into your project so jito loads it
cat > JITO.md <<'EOF'
# My App
Stack: Go 1.25, PostgreSQL 16, OpenAI API.
Style: Tab-indented, errors are wrapped with %w.
EOF

# 5. Run
jito run --mode=dev "add a /healthz endpoint to the server"
```

That's it. For interactive work, `jito chat` opens the Bubble Tea
TUI. For multi-agent work, `jito spawn jito-test "run unit tests"`.

---

## 5 · Features matrix

| Area | v0.1.0 (MVP) | **v0.2.0** (this release) |
|---|---|---|
| Modes | 5 personas, hard-coded system prompt | 5 personas + per-mode rubric tuning |
| Context | none | **`JITO.md` loader** with `.jitoignore` |
| Custom commands | none | **TOML** commands in `~/.jito/commands/` |
| Permissions | `audit`-only | **policy modes** (`allow` / `ask` / `deny` / `audit`) + **picker modal** |
| Sandbox | none | **canonicalise + env scrub + network block + dangerous-cmd deny** (47 checks) |
| Checkpoint / resume | none | **SQLite** sessions, **audit log** with OWASP redaction |
| Multi-agent | basic `agent.Spawn` | **`internal/loop`** wire-up + `jito spawn` + run-log appender |
| Heartbeat | ad-hoc | `internal/heartbeat` pkg (95.5% cov) + `jito heartbeat` CLI |
| Cross-platform | source-only | **goreleaser** 6-target matrix, tar.gz / zip |
| Coverage | n/a | ≥ 90% per pkg with NEW code |
| Test surface | unit only | unit + race-clean + smoke + sandbox regression |

---

## 6 · Architecture

```
jito/
├── cmd/jito/                 entrypoint (main.go → cli.NewRootCmd)
├── internal/
│   ├── agent/                worktree + spawn (sub-process launcher w/ Heartbeat hook)
│   ├── cli/                  cobra commands (root, run, chat, init, heartbeat,
│   │                         version, memory, resume, sessions, doctor, update,
│   │                         worktree, loop, spawn)
│   ├── commands/             TOML custom-slash loader + registry + shlex
│   ├── config/               YAML config (~/.jito/config.yaml)
│   ├── context/              JITO.md hierarchical loader (analog of CLAUDE.md)
│   ├── heartbeat/            2-minute race-safe append-only ticker
│   ├── loop/                 ★ NEW · strict run-log appender for CEO-Profile
│   │                         Loop Engineering layer
│   ├── mcp/                  Model Context Protocol client
│   ├── mode/                 persona router (dev/reason/create/audit/universal)
│   ├── permissions/          policy + approval picker + sandbox
│   ├── plugin/               pluggable tools (JitoPlugin manifest)
│   ├── provider/             OpenAI-compatible client (minimax + openrouter)
│   ├── session/              checkpoint / resume + audit log
│   ├── store/                SQLite (sessions, messages, audit_log)
│   ├── telemetry/            counters + per-mode cost tracking
│   ├── tools/                bash (sandboxed), read, write, edit, web_fetch
│   └── tui/                  Bubble Tea (chat, picker, approval modal)
├── scripts/install.sh        one-shot installer
├── test/                     integration + smoke
├── .goreleaser.yml           ★ NEW · 6-target release pipeline
└── CHANGELOG.md              ★ NEW · auto-gen from conventional commits
```

The Loop Engineering layer that this release wires into lives
**outside** the jito repo at
`~/.openclaw/workspace/state/loop-engineering/`:

```
state/loop-engineering/
├── STATE.md                 last-write-wins scope/budget table
└── run-log-YYYY-MM-DD.md    append-only heartbeat feed
```

---

## 7 · Use cases (UPPU Holdings)

**Chinese wall context.** UPPU Holdings is the parent of four
operating companies, each with its own Chinese wall. jito is the
internal CLI used to drive LLM-assisted work across all four.

| Company | What jito does for it |
|---|---|
| **open-uppu** (this repo) | Builds the IT Master Blueprint (14 systems) — ERP, HRM, CRM, FRM, BI, etc. jito is the multi-agent engine that ships them. |
| `bank` | (out of scope — separate tenant) |
| `sw-house` | (out of scope — separate tenant) |
| `cross` | (out of scope — separate tenant) |

**The Chinese wall is enforced at the filesystem layer.** jito can
only read `~/wokrspace/open-uppu/**` and
`~/.openclaw/workspace/state/loop-engineering/**`; the agent specs
(`agents/jito-*.md`) declare the allow-list in their
`### Filesystem access` row. Cross-company reads are blocked at the
shell level by the BashTool sandbox — the policy mode is `deny` for
any path that doesn't match the allow-list, and `ask` for paths
under `~/` that aren't in the list. The audit log records every
attempt, redacted, into `internal/store`.

**Loop Engineering.** jito-rel (the release engineer sub-agent) is
spawned by PM (main session) on a per-loop cadence. The lifecycle
is fully traceable in the run-log:

```
14:48:15 GMT+7 | LOOP#2 | commands-loader-impl | STARTED
14:53:43 GMT+7 | LOOP#2 | commands-loader-impl | DONE
15:39:30 GMT+7 | LOOP#3 | sandbox-hardening-impl | DONE
20:42:00 GMT+7 | LOOP#3 | jito-sec-LOOP3-respawn-2 | DONE
20:45:00 GMT+7 | LOOP#4 | jito-rel-LOOP4 | STARTED
```

---

## 8 · Contributing + License

**This is an internal project.** Pull requests are accepted only from
uppu-internal contributors. Before opening a PR, read:

- `agents/jito-*.md` — the agent specs that govern the loop pipeline.
- `MEMORY.md` (in this workspace) — accumulated lessons from prior
  loops (don't repeat past mistakes).
- `AGENT-CREATION-CHECKLIST.md` — required fields for new agent specs.
- `state/loop-engineering/STATE.md` — the active scope pivot.

**Coding conventions:**

- Conventional commits (`feat: …`, `fix: …`, `docs: …`, `merge: …`).
- Per-package test coverage ≥ 90% for any package that gains NEW code
  (CEO directive 2026-06-28 12:25).
- `go test -race ./internal/...` must pass.
- The Loop Engineering run-log appender
  (`internal/loop.Engine.Append`) is the source of truth for
  heartbeat lines. Do not write to the run-log by hand — a format
  violation will abort your agent.
- Never `git tag` or `gh release create` without CEO approval
  (jito-rel hard rule #1).

**Release cadence.** The release engineer sub-agent (`jito-rel`)
drafts releases via goreleaser with `--draft`; PM (main session)
reviews and PM (main session) publishes after the CEO human-gate
approves the tag push.

**License.** Private / uppu-internal. See `LICENSE` (TBD) for the
exact text; in the meantime, treat this repository as confidential.

---

<div align="center">

`jito ⚡` — built by the open-uppu Loop Engineering team.
PM (main session) · jito-rel · jito-test · jito-context · jito-commands · jito-session.

</div>
