# jito ⚡

> Multi-mode AI agent CLI for open-uppu Enterprise IT Master Blueprint.
> Powered by Minimax-M3 (OpenAI-compatible API).

```
      _ ____   ___ 
     | (_) |__(_)
  ___| | | '_ \ | |
 / _ \ | | |_) || |
|  __/ | |_.__/_| |
 \___|_|         |_|
```

## What is jito?

`jito` is a production-grade multi-mode AI agent CLI in the spirit of `opencode`, `kilo`, `claude code`, and `codex` — but **mode-aware** and **provider-agnostic**.

It ships with **5 built-in modes**:

| Mode | Purpose |
|------|---------|
| `dev` | Coding, refactoring, debugging |
| `reason` | Planning, analysis, reasoning |
| `create` | Creative writing, marketing copy |
| `audit` | Security review, compliance, code audit |
| `universal` | Catch-all default |

## Quickstart

```bash
# 1. Install (after build)
./scripts/install.sh

# 2. Initialize config
jito init

# 3. Set API key
export JITO_API_KEY=sk-your-minimax-key

# 4. Run
jito run --mode=dev "refactor this function"
jito run --mode=audit "review this PR diff for security issues"
jito run --mode=create "write a tagline for an enterprise IT platform"
jito --mode=reason "design a multi-tenant RBAC schema"
```

## CLI Reference

```
jito [flags] [command]

Flags:
  -m, --mode string     agent mode (default "universal")
  -M, --model string    override model
  -t, --task string     named task
  -v, --verbose         verbose output
      --heartbeat       enable 2-minute heartbeat log
      --config string   config file path

Commands:
  run [prompt]      Single-shot prompt (non-interactive)
  chat              Launch interactive TUI (Phase 2)
  heartbeat         Start 2-minute heartbeat logger
  init              Initialize ~/.jito/ config
  version           Print version
```

## Architecture

```
cmd/jito/                 → entrypoint (main.go)
internal/
  cli/                    → cobra commands (root, run, chat, init, heartbeat, version)
  provider/               → provider abstraction (OpenAI-compatible)
  mode/                   → mode router (dev/reason/create/audit/universal)
  config/                 → YAML config loader
  heartbeat/              → 2-minute tick logger (CEO mandate)
  tui/                    → Bubble Tea interface (Phase 2)
scripts/install.sh        → curl|bash installer
test/smoke_test.sh        → smoke tests
```

## Configuration

`~/.jito/config.yaml`:

```yaml
provider:
  name: minimax
  base_url: https://api.minimax.io/v1
  model: MiniMax-M3
  api_key_env: JITO_API_KEY

fallback_providers:
  - name: openrouter
    base_url: https://openrouter.ai/api/v1
    model: anthropic/claude-3.5-sonnet

mode_default: universal

heartbeat:
  enabled: false
  interval_seconds: 120
  log_dir: ~/.jito/heartbeat
```

## Heartbeat Mandate

Per CEO directive (2026-06-27), every sub-agent must heartbeat every 2 minutes.
jito includes this out of the box:

```bash
jito heartbeat jito-task-1
# writes to ~/.jito/heartbeat/jito-heartbeat-YYYY-MM-DD.log
```

## Build

```bash
go build -o bin/jito ./cmd/jito
./bin/jito --version
```

## Roadmap

- [x] MVP: CLI + 5 modes + provider layer (Phase 1)
- [ ] Bubble Tea TUI (Phase 2)
- [ ] Sub-agent spawning
- [ ] Worktree integration
- [ ] Loop Engineering hooks
- [ ] Plugin system
- [ ] Prebuilt binaries (GitHub Releases)

## License

Private — uppu internal.