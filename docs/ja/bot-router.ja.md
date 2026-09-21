# slack-bot-router — 設計メモ（フューチャーリスト）

> **ステータス**: 未実装。設計思想と仕様の記録。

---

## 概要

`slack-bot-router` は `slack-router` の姉妹プログラムとして、同一リポジトリ内に別バイナリとして実装する予定のデーモンです。

`slack-router` が `/` スラッシュコマンドを対象とした**関数ディスパッチャー**であるのに対し、`slack-bot-router` は `@bot` メンションを対象とした**会話インターフェース**です。両者は設計思想が根本的に異なるため、統合せず独立したプログラムとして維持します。

---

## slack-router との思想的違い

| | slack-router | slack-bot-router |
|---|---|---|
| トリガー | `/command` | `@bot メッセージ` |
| 思想 | 関数呼び出し（決定論的・冪等）| バーチャルユーザーとの対話 |
| 返信責務 | ワーカーが `response_url` に POST | ルーターが Slack API を呼び出す |
| Bot Token | ワーカーに渡さない（不要）| ルーターが保持・使用（ワーカーには渡さない）|
| ワーカーの返信数 | 基本1回 | 複数回可（プロセス終了まで継続）|

---

## アーキテクチャ

```
[Slack]
  │  @bot メッセージ (app_mention)
  ▼
[slack-bot-router]          ← Bot Token を保持
  │  stdin に JSON を1回書き込む
  ▼
[worker script]             ← Slack を一切知らない
  │  stdout に JSON を1行ずつ複数回書き込む
  │  処理完了後 exit 0
  ▼
[slack-bot-router]
  │  stdout の各行を Slack API (chat.postMessage) に順次投稿
  ▼
[Slack]
```

### ワーカーが完全に Slack 非依存になる理由

`slack-router` ではワーカーが `response_url` に直接 HTTP POST するため、Slack のプロトコルを知っている必要があります。`slack-bot-router` ではルーターがすべての Slack API 呼び出しを代行するため、ワーカーは `stdin` を読んで `stdout` に書くだけです。Bot Token もワーカーに渡りません。

---

## ワーカープロトコル仕様

### stdin（ルーター → ワーカー）: 1回

```json
{
  "user_id":    "U123456",
  "channel_id": "C123456",
  "text":       "@bot を除いたメッセージ本文",
  "event_ts":   "1234567890.123456",
  "thread_ts":  "1234567890.000000"
}
```

- `text`: `<@BOTID>` を除去したテキスト
- `event_ts`: メンションメッセージのタイムスタンプ
- `thread_ts`: スレッド返信する場合のルート TS。スレッド外のメンションでは `event_ts` と同値

### stdout（ワーカー → ルーター）: 0回以上

**NDJSON（改行区切り JSON）形式。1行 = 1メッセージ。**

```jsonl
{"text": "調べています...", "response_type": "ephemeral"}
{"text": "完了しました！", "blocks": [...], "response_type": "in_channel"}
```

| フィールド | 必須 | 説明 |
|---|---|---|
| `text` | どちらか必須 | プレーンテキストメッセージ |
| `blocks` | どちらか必須 | Slack Block Kit の配列。`text` と併用可（fallback として使われる）|
| `response_type` | 任意 | `"ephemeral"`（本人のみ表示）または `"in_channel"`（デフォルト）|

ルーターは stdout の JSON フィールドを `chat.postMessage` にそのまま渡す。`thread_ts` 等の追加フィールドもパススルーされる予定。

### 終了シグナル

| 終了パターン | ルーターの動作 |
|---|---|
| `exit 0` | 正常終了。通知なし |
| `exit N`（N > 0）| ワーカーが意図的に終了。ワーカー自身が stdout にエラーメッセージを書いていることを期待。ルーターは追加通知しない |
| シグナル終了（ExitCode < 0）| ワーカーが通知できないまま終了。ルーターがエラーメッセージを投稿 |

> `slack-router` と同じ exit code 規約を継承する。

---

## 実装スコープ（初版）

### 対応する
- テキストメッセージ（`text`）
- Slack Block Kit（`blocks`）
- スレッド返信（`thread_ts`）
- 複数回の stdout 送信（プログレッシブレスポンス）
- タイムアウト・プロセスグループ管理（`slack-router` と同方式）
- グレースフルシャットダウン

### 対応しない（初版スコープ外）
- ファイル添付 — `files.upload` が複数 API ステップを要するうえ、バイナリを stdout 経由で渡す手段がなく、シンプル実装と相容れない
- インタラクティブコンポーネント（ボタンのコールバック等）
- DM（ダイレクトメッセージ）への対応
- 会話履歴の自動管理（必要ならワーカー側が実装する）

---

## 実装上の注意点（将来の実装者へ）

- `app_mention` イベントは Slack Events API 経由で届く。Socket Mode では `EventTypeEventsAPI` として受信し、内部イベントタイプが `app_mention` であることを確認してからディスパッチする
- ルーターが `<@BOTID>` を除去する際、Bot ID は起動時に `auth.test` API で取得しておく
- stdout の行読み取りは `bufio.Scanner` で行単位に行い、各行を JSON パースして即時投稿する（プロセス終了を待たない）
- `chat.postMessage` に渡す `thread_ts` は `event_ts` を使う（スレッド内メンションの場合は元の `thread_ts`）
- コマンドルーティングは「メンション後の最初の単語」を使う。ルーティング不一致の場合はデフォルトルートにフォールバックするか、エラーを返す設計とする

---

## 同一リポジトリでの配置案

```
slack-router/
├── main.go              ← slack-router（既存）
├── router.go
├── worker.go
├── ...
├── cmd/
│   └── bot-router/
│       ├── main.go      ← slack-bot-router（将来実装）
│       └── ...
└── docs/
    ├── en/
    │   ├── slack-setup.md
    │   └── bot-router.md
    └── ja/
        ├── slack-setup.ja.md
        └── bot-router.ja.md    ← このファイル
```

`package main` を維持しつつ `cmd/` サブディレクトリで複数バイナリを管理する標準的な Go プロジェクト構成を採用する。
