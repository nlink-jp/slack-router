# Slack App セットアップガイド

slack-router を動かすには、Slack App の作成とトークンの取得が必要です。このドキュメントでは Slack の管理画面での手順をステップごとに説明します。

---

## 前提

- Slack ワークスペースの管理者権限、またはアプリのインストール権限
- slack-router をインストールするサーバー（または開発環境）

---

## Step 1: Slack App を作成する

1. [Slack API: Your Apps](https://api.slack.com/apps) にアクセスし、**Create New App** をクリック
2. **From scratch** を選択
3. **App Name** に任意の名前を入力（例: `slack-router`）
4. インストール先の **Workspace** を選択
5. **Create App** をクリック

---

## Step 2: Socket Mode を有効化する

1. 左サイドバーの **Settings > Socket Mode** をクリック
2. **Enable Socket Mode** をオン
3. **Token Name** に任意の名前を入力（例: `socket-mode-token`）
4. **Generate** をクリック
5. 表示された `xapp-1-...` のトークンをコピーして保管する

> このトークンが環境変数 `SLACK_APP_TOKEN` に入ります。

---

## Step 3: Bot Token スコープを設定する

1. 左サイドバーの **Features > OAuth & Permissions** をクリック
2. **Scopes > Bot Token Scopes** セクションで **Add an OAuth Scope** をクリック
3. 以下のスコープを追加する

| スコープ | 用途 |
|---|---|
| `commands` | Slash Command の受信 |

> ワーカースクリプトから Slack API（メッセージ投稿など）を直接呼び出す場合は、用途に応じて `chat:write` などを追加してください。ただし slack-router 本体は Slack API を直接呼びません。

---

## Step 4: ワークスペースにインストールする

1. 左サイドバーの **Settings > Install App** をクリック
2. **Install to Workspace** をクリック
3. 権限の確認画面で **許可する** をクリック
4. インストール完了後、**Bot User OAuth Token**（`xoxb-...`）をコピーして保管する

> このトークンが環境変数 `SLACK_BOT_TOKEN` に入ります。

---

## Step 5: Slash Command を登録する

ルーティングしたいコマンド分だけ以下の手順を繰り返します。

1. 左サイドバーの **Features > Slash Commands** をクリック
2. **Create New Command** をクリック
3. 以下を入力する

| 項目 | 入力例 | 説明 |
|---|---|---|
| **Command** | `/ask` | スラッシュコマンド名 |
| **Request URL** | `https://example.com/` | Socket Mode では使用されないため任意の URL で可 |
| **Short Description** | LLM に質問する | Slack 上に表示される説明文 |
| **Usage Hint** | `[質問内容]` | `/ask` の後ろに続くパラメータのヒント（任意） |

4. **Save** をクリック

> `config.yaml` の `routes[].command` と完全一致している必要があります（例: `/ask`）。

---

## Step 6: Event Subscriptions（Socket Mode では設定不要）

Socket Mode を使う場合、イベントを受け取るエンドポイント URL の設定は**不要**です。
Slack から slack-router へのコネクションは、slack-router 側から能動的に確立されます。

---

## Step 7: トークンを設定する

トークンは **環境変数で渡すことを推奨**します。`config.yaml` をリポジトリにコミットしていても、トークンが漏洩するリスクを排除できます。

```bash
cp .env.example .env
```

`.env` を開き、Step 2 と Step 4 で取得したトークンを記入します。

```bash
SLACK_APP_TOKEN=xapp-1-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
SLACK_BOT_TOKEN=xoxb-xxxxxxxxxxxx-xxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxxxxxx
```

> `.env` は `.gitignore` により Git 管理対象外です。絶対にコミットしないでください。

`config.yaml` 側のトークン欄は空のままで構いません。

```bash
cp config.example.yaml config.yaml
# slack.app_token / bot_token は空欄のまま — 環境変数が優先されます
```

---

## Step 8: 起動して接続を確認する

```bash
# 環境変数を読み込んで起動
set -a && source .env && set +a
./slack-router -config config.yaml
```

以下のようなログが出れば接続成功です。

```json
{"time":"2026-03-14T10:00:00Z","level":"INFO","msg":"slack-router starting","version":"v0.1.1","commit":"abc1234","build_date":"2026-03-14T10:00:00Z","routes":1,"max_concurrent_workers":10}
{"time":"2026-03-14T10:00:01Z","level":"INFO","msg":"connecting to slack"}
{"time":"2026-03-14T10:00:01Z","level":"INFO","msg":"connected to slack"}
```

Slack の任意のチャンネルで登録した Slash Command を実行し、ワーカースクリプトが起動することを確認してください。

---

## トラブルシューティング

### `invalid_auth` エラーが出る

- `SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN` が正しく設定されているか確認する
- App が対象ワークスペースにインストールされているか確認する

### `slack app token is not set` エラーが出る

- 環境変数 `SLACK_APP_TOKEN` が設定されているか確認する（`echo $SLACK_APP_TOKEN`）
- または `config.yaml` の `slack.app_token` に値が入っているか確認する

### コマンドを実行しても何も起きない

- Slash Command の **Command** 欄と `config.yaml` の `routes[].command` が一致しているか確認する（大文字・小文字・スラッシュを含めて完全一致）
- `log_level: "debug"` に変更してログを確認する

### `socket mode is not enabled` エラーが出る

- Slack App の設定画面で **Socket Mode** が有効になっているか確認する
- `SLACK_APP_TOKEN` に `xapp-` から始まるトークンを使用しているか確認する（`xoxb-` と混同しないこと）

### 起動時に `script not executable` や `no such file` エラーが出る

slack-router は起動時にすべてのスクリプトを検証します。

| エラー | 対処 |
|---|---|
| `no such file or directory` | `routes[].script` のパスが正しいか確認する |
| `not executable` | `chmod +x ./scripts/your_script.sh` を実行する |
| `world-writable` | `chmod o-w ./scripts/your_script.sh` を実行する |

### ワーカースクリプトは起動するが Slack に返信が来ない

- スクリプトが `response_url` に正しく POST しているか確認する
- `response_url` は `https://hooks.slack.com/` から始まる URL のみ有効です（セキュリティ上の制限）
- スクリプトの終了コードを確認する（`log_level: "debug"` でログに出る）

---

## トークンの管理について

slack-router はトークンを環境変数（`SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN`）から読み込むことを推奨しています。

- `.env` ファイルを使う場合は `.gitignore` で除外されていることを確認する
- ファイルのパーミッションを制限する（`chmod 600 .env`）
- `config.yaml` にトークンを直接書く場合も同様に `chmod 600 config.yaml` を推奨
