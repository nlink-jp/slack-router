# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`--version` flag** (also `-version`). Prints
  `slack-router <version> (commit <commit>, built <date>)` and exits 0
  without loading a config file. The binary previously had no such flag, so
  `make verify-release` — which runs the packaged binary's `--version` and
  requires the tag in its output — refused every real release with
  "flag provided but not defined: -version".

### Fixed

- **`make verify-release` now fails closed.** Its last block chained unzip, the
  packaged binary's `--version` and `spctl` with `&&` and ended the whole chain
  in `|| true`, so a zip that did not unpack or a binary that did not run exited
  0 and the upload proceeded. Each step is now judged on its own, the packaged
  binary's `--version` must contain the tag being released, and only the
  informational `spctl` line may be ignored. Matches the org template
  (CONVENTIONS.md §Code Signing → Verifying a release).
- **The Linux archives no longer carry macOS file metadata.** macOS `tar` wrote
  each bundled file's extended attributes (`com.apple.provenance`, and a Dropbox
  attribute where the tree is synced) into the `.tar.gz` twice: as AppleDouble
  `._` members, which GNU tar extracts as stray `._<name>` files beside the real
  ones, and as `LIBARCHIVE.xattr.*` / `SCHILY.xattr.*` pax headers, which it
  reports as unknown keywords. `make release` now archives with
  `COPYFILE_DISABLE=1 tar --no-xattrs`; each setting stops one of the two.
  Archives already published still carry them; the files themselves are
  unaffected.

### Internal

- `make verify-release` also judges each Linux archive: no AppleDouble or other
  macOS metadata members — listed with `--options 'tar:!mac-ext'`, because a
  plain macOS listing folds `._` members away — no extended attributes as pax
  headers, and exactly the binary and the bundled files (`BUNDLE_FILES`) under
  `<name>/`, compared in the C locale.

## [0.3.0] - 2026-07-12

### Removed

- **darwin/amd64 (Intel) pre-built binary.** macOS releases now ship
  **arm64 only**, per the org-wide policy (darwin is Apple-Silicon only; no
  universal binaries). Intel Mac users can build from source.

### Changed

- **Linux release archives are now `.tar.gz`** (darwin remains `.zip`), per
  `nlink-jp/.github` CONVENTIONS.md §Release Archive Standard. Each archive
  still expands to a `slack-router-vX.Y.Z-<os>-<arch>/` directory bundling the
  binary with `README.md`, `docs/`, the routed `scripts/`, and the config
  examples (this daemon intentionally ships a self-contained bundle).
- **`LICENSE` is now bundled** in every release archive.
- **Dropped the `-s -w` linker strip flags** from `LDFLAGS`, aligning with the
  org-standard build flags (slack-router has no Windows build, so this is
  purely a consistency change).

No change to the binary's behaviour — a packaging / build-config release.

## [0.2.1] - 2026-05-22

### Changed

- **Darwin releases are now Developer ID signed and Apple-notarized.**
  `slack-router-v0.2.1-darwin-{amd64,arm64}.zip` carry full Apple
  Developer ID Application signatures and notarization tickets from
  Apple. End users on macOS no longer need to bypass Gatekeeper
  with right-click → Open or `xattr -d com.apple.quarantine` on
  first launch; local users who place `slack-router` under
  Dropbox-synced (or any other FileProvider-managed) paths are no
  longer killed by macOS's ad-hoc + provenance distrust policy.
  Pipeline: `build-tools/codesign-darwin.sh` +
  `build-tools/notarize-darwin.sh`, driven by `make release`.
  Adopts the org-wide convention in `nlink-jp/.github`
  CONVENTIONS.md §Code Signing.

### Internal

- **Build-time helpers live in `build-tools/`, not `scripts/`.**
  slack-router is the org's only project that bundles `scripts/`
  into the release zip (the routed command samples ship there).
  Putting codesign/notarize under `scripts/` would either pollute
  the distribution or require fragile by-name filtering of
  `BUNDLE_FILES`. Locating them under `build-tools/` keeps the
  release artifact pure without any inclusion-list maintenance.

No behaviour change to the binary itself — feature-wise this is
identical to v0.2.0.

## [0.2.0] - 2026-03-28

### Changed
- Migrated to nlink-jp organisation — module path updated to `github.com/nlink-jp/slack-router`
- Added English README (`README.md`); existing Japanese README moved to `README.ja.md`
- Added `package` Makefile target as alias for `release`

## [0.1.5] - 2026-03-14

### Added

- **ワーカー異常終了時の通知規約（exit code による振り分け）** — `exec.ExitError.ExitCode()` の値でルーターの通知有無を制御。`exit 1` 等の正の終了コードはスクリプトが意図的に終了したとみなし通知しない（スクリプト自身が `response_url` で返信済みと期待）。シグナルによる強制終了（OOM killer・外部 SIGKILL 等）は `ExitCode < 0` となり、ルーターが `error_message` を送信

### Changed

- **`error_message` のデフォルト文字列を汎用表現に変更** — 起動失敗・シグナル終了どちらにも対応できる「予期しないエラーが発生しました」に統一

### Documentation

- README にエラー時の通知規約テーブルとスクリプト実装規約を追記
- AGENTS.md に exit code 規約と通知ロジックを追記

## [0.1.4] - 2026-03-14

### Added

- **ハートビートログ** — 定期的に `{"msg":"heartbeat","uptime":"..."}` を出力し、ログ監視システムによるプロセス死活監視を可能にする。間隔は `global.heartbeat_interval`（デフォルト `1m`、`0` で無効）で設定可能。起動ログにも設定値を記録

## [0.1.3] - 2026-03-14

### Added

- **ワーカー起動失敗時のユーザー通知** — `cmd.Start` 失敗・stdin パイプ失敗・JSON エンコード失敗など、ルーター起因のエラー発生時に ephemeral メッセージでユーザーへ通知するよう修正。ACL/輻輳エラーとの UX 一貫性を確保。通知メッセージはルートごとに `error_message` で設定可能（省略時はデフォルト文字列）
- **ユニットテスト** — `acl_test.go`・`worker_test.go`・`config_test.go` を追加。`ACL.Check` / `ACL.isEmpty` / `validateResponseURL` / `sanitizedEnv` / `validateScript` / `LoadConfig` をカバー。プロダクションコードの変更なし

### Fixed

- **ワーカーの stdout/stderr がサイレント破棄される問題** — `cmd.Stdout`/`cmd.Stderr` を未設定のままにしていたため、デーモン実行時にワーカーの全出力が /dev/null 相当に捨てられていた。`bytes.Buffer` で捕捉し、stdout を INFO・stderr を WARN レベルで構造化ログに出力するよう修正

### Changed

- **`make test` にレースデテクタを追加** — `go test -race ./...` に変更し、並行バグを早期検出
- **`make lint` に `staticcheck` を追加** — インストール済みの場合のみ実行し、未インストール時はインストール案内を表示

## [0.1.2] - 2026-03-14

### Security

- **ワーカープロセスへの Slack トークン漏洩を防止** — `exec.Command` はデフォルトで親プロセスの環境変数を引き継ぐため、`SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN` がワーカースクリプトに渡ってしまっていた。`cmd.Env` を明示的に設定し、機密の環境変数をブロックリストで除去するよう修正。`PATH` や `HOME` など汎用的な変数は引き続きワーカーに渡される

## [0.1.1] - 2026-03-14

### Fixed

- **グレースフルシャットダウン中の新規リクエスト受付競合** — `wg.Add` と `wg.Wait` の間に存在した race condition を mutex で解消。シャットダウン開始後に到着したリクエストが WaitGroup に追加されワーカーが孤立する問題を修正
- **シャットダウン時の ephemeral 通知ロスト** — `notifyEphemeral` の goroutine が WaitGroup 管理外だったため、HTTP POST 完了前にプロセスが終了してユーザーへの通知が届かない場合があった。専用の `notifyWg` で追跡しシャットダウン完了まで待機するよう修正

### Changed

- **`sliceContains` を `slices.Contains` に置換** — Go 1.21 標準ライブラリの `slices` パッケージを使用し、自前実装を削除

## [0.1.0] - 2026-03-14

### Added

- **Slack Socket Mode 接続** — インバウンドポート開放不要でイベントを受信
- **YAML ルーティングテーブル** — コマンドとスクリプトの紐づけを設定ファイルで管理
- **安全なパラメータ渡し** — コマンドメタデータを `stdin` 経由の JSON で渡す（argv を使わずプロセス一覧からの情報漏洩を防止）
- **グローバル / ルート別同時実行制御** — チャンネルセマフォ方式で DoS を防止
- **タイムアウト強制停止** — SIGTERM → 5秒待機 → SIGKILL でプロセスツリーごと終了
- **ACL（アクセス制御リスト）** — ルート単位で allow/deny チャンネル・ユーザーを設定可能
- **設定可能なユーザー向けメッセージ** — 拒否・輻輳時のメッセージを config.yaml から変更可能
- **ephemeral 通知** — 拒否・エラーメッセージはリクエストしたユーザーにのみ表示
- **環境変数によるトークン注入** — `SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN` で config.yaml をトークンフリーに保てる
- **起動時スクリプト検証** — 存在確認・実行権限・world-writable チェックをデーモン起動時に実施
- **スクリプトパスの絶対化** — config ファイル基準で解決し CWD 非依存に
- **構造化 JSON ログ** — `log/slog` による version / commit / PID などのフィールド付きログ
- **グレースフルシャットダウン** — SIGINT/SIGTERM 受信後、実行中ワーカーの完了を待機してから終了
- **クロスプラットフォームビルド** — macOS (amd64/arm64)・Linux (amd64/arm64)・Windows (amd64) に対応
- **ビルド時バージョン埋め込み** — `git describe --tags` の結果を `-ldflags` でバイナリに埋め込み
- **サンプルスクリプト** — `scripts/hello.sh`（挨拶スクリプト）を同梱

[Unreleased]: https://github.com/nlink-jp/slack-router/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/nlink-jp/slack-router/compare/v0.1.5...v0.2.0
[0.1.5]: https://github.com/nlink-jp/slack-router/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/nlink-jp/slack-router/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/nlink-jp/slack-router/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/nlink-jp/slack-router/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/nlink-jp/slack-router/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/nlink-jp/slack-router/releases/tag/v0.1.0
