# slack-router

`slack-router` is a daemon that receives Slack Slash Commands via Socket Mode and asynchronously dispatches them to local shell scripts according to a routing table defined in a YAML configuration file.

It serves as a hub for ChatOps scenarios such as system administration, LLM integration, and deploy automation — providing a safe, scalable, and decoupled execution model.

[日本語版 README はこちら](README.ja.md)

## Features

- **Socket Mode** — receives events without exposing inbound ports
- **Command routing** — maps Slash Commands to scripts via a YAML config file
- **Safe parameter passing** — passes command metadata as JSON via `stdin` (not `argv`), preventing credential leakage via `ps aux`
- **DoS protection** — global and per-command concurrency limits (semaphores)
- **Forced timeout** — SIGTERM → 5-second grace period → SIGKILL terminates the entire process tree
- **ACL** — per-route allow/deny lists for channels and users
- **Ephemeral notifications** — deny and error messages are sent only to the requesting user
- **Configurable messages** — deny and busy messages can be customized in `config.yaml`
- **Environment variable token injection** — `SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN` keep config files token-free
- **Startup script validation** — checks for existence, execute permission, and world-writable status at daemon start
- **Structured JSON logging** — includes version, commit, PID, and other fields
- **Graceful shutdown** — waits for in-flight workers and notification goroutines before exiting on SIGINT/SIGTERM

## Installation

Download the appropriate zip for your platform from the [Releases](https://github.com/nlink-jp/slack-router/releases) page and extract it.

```bash
unzip slack-router-vX.Y.Z-darwin-arm64.zip
cd slack-router-vX.Y.Z-darwin-arm64
```

## Configuration

### Token setup

Set tokens via environment variables (recommended — keeps `config.yaml` safe to commit):

```bash
cp .env.example .env
# Edit .env and fill in your tokens
```

```bash
# .env (git-ignored)
SLACK_APP_TOKEN=xapp-1-...
SLACK_BOT_TOKEN=xoxb-...
```

Load environment variables at startup:

```bash
set -a && source .env && set +a
./slack-router -config config.yaml
```

Environment variables always take precedence over values in `config.yaml`.

### Configuration file

```bash
cp config.example.yaml config.yaml
```

```yaml
slack:
  app_token: ""  # or SLACK_APP_TOKEN env var
  bot_token:  ""  # or SLACK_BOT_TOKEN env var

global:
  max_concurrent_workers: 10
  log_level: "info"
  messages:
    server_busy: ":warning: The server is busy. Please try again later."

routes:
  - command: "/hello"
    script:  "./scripts/hello.sh"
    timeout: "10s"
    max_concurrency: 5
```

For the full Slack App setup guide, see [docs/en/slack-setup.md](docs/en/slack-setup.md).

### Configuration reference

| Key | Required | Default | Description |
|---|---|---|---|
| `slack.app_token` / `SLACK_APP_TOKEN` | ✓ | — | App-Level Token (`xapp-...`) |
| `slack.bot_token` / `SLACK_BOT_TOKEN` | ✓ | — | Bot Token (`xoxb-...`) |
| `global.max_concurrent_workers` | | `10` | Global concurrency limit |
| `global.log_level` | | `info` | `debug` / `info` / `warn` / `error` |
| `global.heartbeat_interval` | | `1m` | Heartbeat log interval (`0` to disable) |
| `global.messages.server_busy` | | default string | Message sent when global limit is reached |
| `routes[].command` | ✓ | — | Slash Command name (e.g. `/ask`) |
| `routes[].script` | ✓ | — | Script path (relative to config file) |
| `routes[].timeout` | | `5m` | Timeout in Go duration format (`30s`, `5m`, `1h`) |
| `routes[].max_concurrency` | | unlimited | Per-command concurrency limit |
| `routes[].busy_message` | | default string | Message sent when route limit is reached |
| `routes[].deny_message` | | default string | Message sent on ACL denial |
| `routes[].error_message` | | default string | Message sent on worker start failure |
| `routes[].allow_channels` | | unlimited | List of channel IDs allowed to run the command |
| `routes[].allow_users` | | unlimited | List of user IDs allowed to run the command |
| `routes[].deny_channels` | | none | List of channel IDs blocked from running the command |
| `routes[].deny_users` | | none | List of user IDs blocked from running the command |

### Access Control (ACL)

ACL evaluation order (highest priority first):

| Order | Rule | If list is empty |
|---|---|---|
| 1 | `deny_users` | Skip (all users pass) |
| 2 | `deny_channels` | Skip (all channels pass) |
| 3 | `allow_users` | Allow all users |
| 4 | `allow_channels` | Allow all channels |

Deny always takes precedence over allow. An empty allow list means "allow all".

## Usage

### Starting the daemon

```bash
./slack-router -config config.yaml
```

| Flag | Default | Description |
|---|---|---|
| `-config` | `config.yaml` | Path to config file |
| `--version` | | Print the version, commit, and build date, then exit (no config file needed) |

```bash
$ ./slack-router --version
slack-router v0.3.1 (commit abc1234, built 2026-09-23T00:00:00Z)
```

### Worker scripts

The router starts your script and writes the following JSON to its `stdin`:

```json
{
  "command":      "/ask",
  "text":         "hello",
  "user_id":      "U123456",
  "channel_id":   "C123456",
  "response_url": "https://hooks.slack.com/commands/..."
}
```

A sample script is provided at [`scripts/hello.sh`](scripts/hello.sh). See the Japanese README for detailed scripting guidelines.

### Exit code conventions

| Exit pattern | Exit code | Router notification |
|---|---|---|
| Normal exit | `0` | None |
| `exit 1` / `exit 2` etc. | `> 0` | None — script is expected to notify via `response_url` |
| Killed by signal (OOM, external SIGKILL) | `< 0` | Sends `error_message` to user |
| Worker launch failure | — | Sends `error_message` to user |

## Building

```bash
make build    # Current platform → ./slack-router
make package  # All platforms (macOS arm64, Linux amd64/arm64) → dist/
make test     # go test -race ./...
make lint     # go vet + staticcheck
make clean    # Remove binary and dist/
```

## License

MIT © [magifd2](https://github.com/magifd2)
