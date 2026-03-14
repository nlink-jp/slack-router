# slack-router

Slack の Slash Command を Socket Mode で受け取り、設定ファイルのルーティングテーブルに従ってローカルのシェルスクリプトをサブプロセスとして非同期実行するデーモンです。

システム管理・LLM 連携・デプロイ自動化などの ChatOps を安全かつスケーラブルに実現するハブとして機能します。

---

## 機能

- **Socket Mode 接続** — インバウンドのポート開放不要でイベントを受信
- **コマンドルーティング** — YAML 設定ファイルで Slash Command とスクリプトを紐づけ
- **安全なパラメータ渡し** — コマンドメタデータを `stdin` 経由の JSON で渡す（argv を使わないため `ps aux` からの情報漏洩を防止）
- **DoS 対策** — グローバルおよびコマンド単位の同時実行数上限（セマフォ）
- **タイムアウト強制停止** — SIGTERM → 5秒待機 → SIGKILL でプロセスツリーごと終了
- **ACL** — ルート単位で allow/deny チャンネル・ユーザーを設定
- **Ephemeral 通知** — 拒否・エラーメッセージはリクエストしたユーザーにのみ表示
- **設定可能なメッセージ** — 拒否・輻輳時の文言を `config.yaml` から変更可能
- **環境変数によるトークン注入** — `SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN` で設定ファイルをトークンフリーに保てる
- **起動時スクリプト検証** — 存在確認・実行権限・world-writable チェックをデーモン起動時に実施
- **構造化 JSON ログ** — バージョン / コミット / PID などのフィールド付き
- **グレースフルシャットダウン** — SIGINT/SIGTERM 受信後、実行中ワーカーと通知 goroutine の完了を待機してから終了

---

## 必要要件

**バイナリを使う場合（推奨）**
- macOS または Linux
- Slack App（Socket Mode 有効・Slash Command 登録済み）

**ソースからビルドする場合**
- 上記に加えて Go 1.22 以上

Slack App の設定手順は [docs/slack-setup.md](docs/slack-setup.md) を参照してください。

---

## インストール

### リリースバイナリを使う（推奨）

[Releases](https://github.com/magifd2/slack-router/releases) からお使いの環境に合わせた zip をダウンロードして展開します。

```bash
unzip slack-router-v0.1.2-darwin-arm64.zip
cd slack-router-v0.1.2-darwin-arm64
```

### ソースからビルドする

```bash
git clone https://github.com/magifd2/slack-router.git
cd slack-router
make build
```

バイナリ (`./slack-router`) が生成されます。依存関係はバイナリに同梱されるため、配置先サーバーに Go 環境は不要です。

---

## 設定

### トークンの設定

**環境変数で渡すことを推奨します。** `config.yaml` をリポジトリに含めても安全になります。

```bash
cp .env.example .env
# .env を開いてトークンを記入
```

```bash
# .env（Git 管理対象外）
SLACK_APP_TOKEN=xapp-1-...
SLACK_BOT_TOKEN=xoxb-...
```

起動時に環境変数を読み込む例:

```bash
set -a && source .env && set +a
./slack-router -config config.yaml
```

環境変数が設定されている場合は常に `config.yaml` の値より優先されます。

### 設定ファイル

```bash
cp config.example.yaml config.yaml
```

```yaml
slack:
  app_token: ""  # または環境変数 SLACK_APP_TOKEN
  bot_token:  ""  # または環境変数 SLACK_BOT_TOKEN

global:
  max_concurrent_workers: 10
  log_level: "info"
  messages:
    server_busy: ":warning: サーバーが混み合っています。後でお試しください。"

routes:
  # 同梱の挨拶サンプル（scripts/hello.sh）
  - command: "/hello"
    script:  "./scripts/hello.sh"
    timeout: "10s"
    max_concurrency: 5

  # ACL を使ったルートの例（script は別途用意が必要）
  # - command: "/deploy"
  #   script:  "./scripts/deploy.sh"
  #   timeout: "30m"
  #   max_concurrency: 1
  #   allow_channels:
  #     - "C000000001"  # #ops のみ
  #   allow_users:
  #     - "U000000001"  # @alice
  #   busy_message: ":warning: デプロイは同時に1件のみです。完了後に再試行してください。"
  #   deny_message:  ":no_entry: デプロイの実行権限がありません。"
```

### 設定項目リファレンス

| キー | 必須 | デフォルト | 説明 |
|---|---|---|---|
| `slack.app_token` / `SLACK_APP_TOKEN` | ✓ | — | `xapp-` から始まる App-Level Token |
| `slack.bot_token` / `SLACK_BOT_TOKEN` | ✓ | — | `xoxb-` から始まる Bot Token |
| `global.max_concurrent_workers` | | `10` | 全コマンド合計の同時実行上限 |
| `global.log_level` | | `info` | `debug` / `info` / `warn` / `error` |
| `global.heartbeat_interval` | | `1m` | 死活監視用ハートビートログの間隔（`0` で無効） |
| `global.messages.server_busy` | | デフォルト文字列 | グローバル上限到達時のメッセージ |
| `routes[].command` | ✓ | — | Slash Command 名（例: `/ask`） |
| `routes[].script` | ✓ | — | 実行するスクリプトのパス（相対パスは config ファイル基準） |
| `routes[].timeout` | | `5m` | タイムアウト（Go の duration 形式: `30s`, `5m`, `1h`） |
| `routes[].max_concurrency` | | 無制限 | このコマンドの同時実行上限 |
| `routes[].busy_message` | | デフォルト文字列 | ルート上限到達時にユーザーへ送るメッセージ |
| `routes[].deny_message` | | デフォルト文字列 | ACL 拒否時にユーザーへ送るメッセージ |
| `routes[].error_message` | | デフォルト文字列 | ワーカー起動失敗時にユーザーへ送るメッセージ |
| `routes[].allow_channels` | | 無制限 | 実行を許可するチャンネル ID のリスト |
| `routes[].allow_users` | | 無制限 | 実行を許可するユーザー ID のリスト |
| `routes[].deny_channels` | | なし | 実行を拒否するチャンネル ID のリスト |
| `routes[].deny_users` | | なし | 実行を拒否するユーザー ID のリスト |

### アクセス制御 (ACL)

各ルートに allow / deny リストを設定することで、コマンドの実行権限をチャンネル・ユーザー単位で制御できます。

**評価順序（優先度高い順）:**

| 順序 | ルール | リストが空の場合 |
|---|---|---|
| 1 | `deny_users` | スキップ（全ユーザー通過） |
| 2 | `deny_channels` | スキップ（全チャンネル通過） |
| 3 | `allow_users` | 全ユーザー許可 |
| 4 | `allow_channels` | 全チャンネル許可 |

- deny は allow より常に優先されます
- allow リストが空（未設定）の場合は「全員許可」として扱います
- 拒否された場合、ユーザーには `deny_message` の内容のみ通知されます（どのルールにマッチしたかは伏せます）

チャンネル ID / ユーザー ID は Slack の URL やプロフィール画面から確認できます（`C` から始まるのがチャンネル、`U` から始まるのがユーザー）。

---

## ワーカースクリプトの書き方

ルーターはスクリプトを起動し、以下の JSON を `stdin` に書き込んで閉じます。スクリプトは `stdin` を読み取り、必要な処理を行います。

動作確認用のサンプルスクリプトとして [`scripts/hello.sh`](scripts/hello.sh) を用意しています。

### stdin に流れてくる JSON

```json
{
  "command":      "/ask",
  "text":         "こんにちは",
  "user_id":      "U123456",
  "channel_id":   "C123456",
  "response_url": "https://hooks.slack.com/commands/..."
}
```

### Bash スクリプトの例

```bash
#!/usr/bin/env bash
set -euo pipefail

# 依存ツールの確認
for cmd in jq curl; do
    command -v "$cmd" > /dev/null 2>&1 || { echo "missing: $cmd" >&2; exit 1; }
done

# stdin から JSON を読み取る
payload=$(cat)
user_id=$(     echo "$payload" | jq -r '.user_id')
text=$(         echo "$payload" | jq -r '.text')
response_url=$( echo "$payload" | jq -r '.response_url')

# response_url の検証（SSRF 対策）
[[ "$response_url" == "https://hooks.slack.com/"* ]] \
    || { echo "invalid response_url" >&2; exit 1; }

# Slack へ ephemeral 返信（jq で JSON を安全に組み立て）
curl -sSf -X POST "$response_url" \
    -H "Content-Type: application/json" \
    -d "$(jq -n --arg text "<@${user_id}>: ${text}" \
             '{"response_type":"ephemeral","text":$text}')"
```

### ルーターとワーカーの責務分担

```
[Slack] ──slash command──▶ [slack-router]
                               │
                               ├─ ACK（3秒以内）
                               ├─ ACL チェック
                               ├─ DoS チェック（セマフォ）
                               └─ script 起動 ──stdin JSON──▶ [worker script]
                                                                    │
                                                                    └─ response_url POST ──▶ [Slack]
```

ルーターは Slack への「返信」に関与しません。返信はワーカースクリプトが `response_url` を通じて行います（疎結合）。

---

## 起動

```bash
./slack-router -config config.yaml
```

| フラグ | デフォルト | 説明 |
|---|---|---|
| `-config` | `config.yaml` | 設定ファイルのパス |

### ログ出力例

```json
{"time":"2026-03-14T10:00:00Z","level":"INFO","msg":"slack-router starting","version":"v0.1.1","commit":"abc1234","build_date":"2026-03-14T10:00:00Z","routes":2,"max_concurrent_workers":10}
{"time":"2026-03-14T10:00:01Z","level":"INFO","msg":"connected to slack"}
{"time":"2026-03-14T10:01:00Z","level":"INFO","msg":"slash command received","command":"/ask","user":"U123456","channel":"C123456"}
{"time":"2026-03-14T10:01:00Z","level":"INFO","msg":"worker started","pid":12345,"command":"/ask","script":"/opt/slack-router/scripts/ask_llm.sh","user":"U123456"}
{"time":"2026-03-14T10:01:02Z","level":"INFO","msg":"worker exited normally","pid":12345,"command":"/ask"}
```

> `text` フィールドはセキュリティ上の理由からログに記録されません。

---

## グレースフルシャットダウン

`SIGINT`（Ctrl+C）または `SIGTERM` を受け取ると、以下の順序でシャットダウンします。

1. 新規リクエストの受付を停止
2. 実行中のワーカープロセスがすべて終了するまで待機
3. 送信中の ephemeral 通知がすべて完了するまで待機
4. プロセス終了

---

## Makefile

```bash
make build    # 現在の OS/アーキ向けにビルド
make release  # 全プラットフォーム向けに dist/ へ zip 出力
make run      # ビルドして起動（config.yaml を使用）
make test     # go test -race ./...
make lint     # go vet + staticcheck
make tidy     # go mod tidy
make version  # 現在のバージョンを表示
make tag      # annotated tag を作成（VERSION=v0.x.y 必須）
make clean    # バイナリと dist/ を削除
```

---

## アーキテクチャ

```
main.go       — Socket Mode イベントループ・シグナルハンドリング
config.go     — YAML 設定の読み込み・バリデーション・スクリプトパス解決
router.go     — コマンドルーティング・グローバル/ルート別セマフォ管理・シャットダウン制御
worker.go     — exec.Command・stdin JSON 注入・タイムアウト処理・Slack 通知
acl.go        — allow/deny リストによるアクセス制御
```

### セマフォの構造

```
[global semaphore]  max_concurrent_workers = 10
    └── [/ask semaphore]    max_concurrency = 3
    └── [/deploy semaphore] max_concurrency = 1
```

上限に達したリクエストは即座にドロップされ、`response_url` 経由でリクエストユーザーにのみ通知されます（ephemeral）。

---

## ライセンス

MIT © [magifd2](https://github.com/magifd2)
