# Slack App setup guide

Running slack-router requires creating a Slack App and obtaining its tokens. This document walks through the steps in Slack's admin screens one at a time.

---

## Prerequisites

- Administrator rights on the Slack workspace, or permission to install apps
- A server (or development environment) to install slack-router on

---

## Step 1: Create a Slack App

1. Go to [Slack API: Your Apps](https://api.slack.com/apps) and click **Create New App**
2. Choose **From scratch**
3. Enter any name in **App Name** (for example `slack-router`)
4. Select the **Workspace** to install it into
5. Click **Create App**

---

## Step 2: Enable Socket Mode

1. Click **Settings > Socket Mode** in the left sidebar
2. Turn **Enable Socket Mode** on
3. Enter any name in **Token Name** (for example `socket-mode-token`)
4. Click **Generate**
5. Copy the `xapp-1-...` token that appears and keep it

> This token goes into the `SLACK_APP_TOKEN` environment variable.

---

## Step 3: Set the Bot Token scopes

1. Click **Features > OAuth & Permissions** in the left sidebar
2. In the **Scopes > Bot Token Scopes** section, click **Add an OAuth Scope**
3. Add the following scope

| Scope | Purpose |
|---|---|
| `commands` | Receiving Slash Commands |

> If a worker script calls the Slack API directly (to post a message, for instance), add scopes such as `chat:write` as its use requires. slack-router itself does not call the Slack API directly.

---

## Step 4: Install it into the workspace

1. Click **Settings > Install App** in the left sidebar
2. Click **Install to Workspace**
3. On the permission confirmation screen, click **Allow**
4. Once the installation completes, copy the **Bot User OAuth Token** (`xoxb-...`) and keep it

> This token goes into the `SLACK_BOT_TOKEN` environment variable.

---

## Step 5: Register the Slash Commands

Repeat the steps below once for each command you want to route.

1. Click **Features > Slash Commands** in the left sidebar
2. Click **Create New Command**
3. Fill in the following

| Field | Example | Description |
|---|---|---|
| **Command** | `/ask` | The slash command name |
| **Request URL** | `https://example.com/` | Not used under Socket Mode, so any URL will do |
| **Short Description** | Ask the LLM a question | The description shown in Slack |
| **Usage Hint** | `[question]` | A hint for the parameters that follow `/ask` (optional) |

4. Click **Save**

> It must match `routes[].command` in `config.yaml` exactly (for example `/ask`).

---

## Step 6: Event Subscriptions (nothing to configure under Socket Mode)

When you use Socket Mode, there is **no need** to configure an endpoint URL to receive events.
The connection from Slack to slack-router is established actively from the slack-router side.

---

## Step 7: Configure the tokens

Passing the tokens **through environment variables is recommended**. That removes the risk of leaking a token even when `config.yaml` is committed to the repository.

```bash
cp .env.example .env
```

Open `.env` and fill in the tokens obtained in Step 2 and Step 4.

```bash
SLACK_APP_TOKEN=xapp-1-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
SLACK_BOT_TOKEN=xoxb-xxxxxxxxxxxx-xxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxxxxxx
```

> `.env` is kept out of Git by `.gitignore`. Never commit it.

The token fields in `config.yaml` can be left empty.

```bash
cp config.example.yaml config.yaml
# leave slack.app_token / bot_token empty — environment variables take precedence
```

---

## Step 8: Start it and check the connection

```bash
# load the environment variables and start
set -a && source .env && set +a
./slack-router -config config.yaml
```

The connection succeeded if logs like these appear.

```json
{"time":"2026-03-14T10:00:00Z","level":"INFO","msg":"slack-router starting","version":"v0.1.1","commit":"abc1234","build_date":"2026-03-14T10:00:00Z","routes":1,"max_concurrent_workers":10}
{"time":"2026-03-14T10:00:01Z","level":"INFO","msg":"connecting to slack"}
{"time":"2026-03-14T10:00:01Z","level":"INFO","msg":"connected to slack"}
```

Run one of the registered Slash Commands in any Slack channel and confirm that the worker script starts.

---

## Troubleshooting

### An `invalid_auth` error appears

- Check that `SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN` are set correctly
- Check that the App is installed into the workspace in question

### A `slack app token is not set` error appears

- Check that the `SLACK_APP_TOKEN` environment variable is set (`echo $SLACK_APP_TOKEN`)
- Or check that `slack.app_token` in `config.yaml` holds a value

### The command runs but nothing happens

- Check that the Slash Command's **Command** field and `routes[].command` in `config.yaml` agree (an exact match, including case and the slash)
- Change `log_level: "debug"` and read the logs

### A `socket mode is not enabled` error appears

- Check that **Socket Mode** is enabled in the Slack App's settings screen
- Check that `SLACK_APP_TOKEN` holds a token beginning with `xapp-` (do not confuse it with `xoxb-`)

### A `script not executable` or `no such file` error appears at startup

slack-router validates every script at startup.

| Error | What to do |
|---|---|
| `no such file or directory` | Check that the `routes[].script` path is correct |
| `not executable` | Run `chmod +x ./scripts/your_script.sh` |
| `world-writable` | Run `chmod o-w ./scripts/your_script.sh` |

### The worker script starts but no reply arrives in Slack

- Check that the script POSTs to `response_url` correctly
- Only a URL beginning with `https://hooks.slack.com/` is valid as a `response_url` (a security restriction)
- Check the script's exit code (it appears in the logs under `log_level: "debug"`)

---

## About managing the tokens

slack-router recommends reading the tokens from environment variables (`SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN`).

- If you use a `.env` file, confirm that it is excluded by `.gitignore`
- Restrict the file's permissions (`chmod 600 .env`)
- If you write a token directly into `config.yaml`, `chmod 600 config.yaml` is recommended for it too
