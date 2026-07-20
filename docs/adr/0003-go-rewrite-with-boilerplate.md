# ADR 0003: Go に全書き換え、`my-boilerplate/go-cli` を土台に採用

## Status

Accepted (v0.2.0)

Supersedes [ADR 0001](0001-single-file-zsh-and-plain-tests.md).

## Context

v0.1.0 は zsh 単一ファイル（`bin/codex-run` 285 行、plain zsh テスト 279 行）でリリースした（ADR 0001）。ADR 0001 の Trigger to revisit（400 行 / 100 assertion / 並列化 / rollout schema 破壊）はいずれも未達だったが、以下の**別の理由**で Go 化を実施する。

### 主因: my-boilerplate 発火漏れの是正

新規 CLI 作成時には `rengotaku/my-boilerplate` の `go-cli` テンプレを scaffold する規範（`ubiterm.my-boilerplate-first`）を、v0.1.0 開始時に**適用し忘れていた**。「zsh の方が MVP コストが低い」という判断で ADR 0001 に落としたが、そもそも boilerplate ベースで始めるべきだった。この発火漏れを是正するために早期に切り替える。

### 副次: 後続機能を Go 前提で設計できる

backlog（#10 session-id ベース rollout 特定、#11 `--format json`、#12 `CODEX_FAIL_PATTERNS` env）はいずれも型ありコードで書いた方が拡張しやすい。特に rollout jsonl parser を Python heredoc の inline 実装から Go の `encoding/json` に移すと、schema 変更に対する型検証が効く。

### 副次: 実行環境依存の削減

zsh 版は実行に `zsh`, `python3`, GNU `timeout` を要求していた。Go 単一バイナリなら `codex` 以外のランタイム依存がゼロになり、`go install github.com/rengotaku/codex-run@latest` で入る。

## Decision

### 実装: Go 単一バイナリ（`my-boilerplate/go-cli` scaffold ベース）

- モジュール: `github.com/rengotaku/codex-run`
- CLI フレームワーク: `github.com/spf13/cobra`（boilerplate 既定）
- テスト: `github.com/stretchr/testify`（boilerplate 既定）
- パッケージレイアウト:
  - `cmd/root.go` — Cobra root。`DisableFlagParsing: true` で未知フラグを passthrough に温存
  - `internal/cli/` — 引数パース（passthrough / `--` / `--models` / `--out` / `--timeout`）
  - `internal/runner/` — fallback loop、`context.WithTimeout` による timeout、`os.CreateTemp`、`regexp` による FAIL_PATTERNS 検出
  - `internal/usage/` — rollout jsonl parser、TSV writer、silent-skip 契約
- 依存: `spf13/cobra`, `sethvargo/go-envconfig`（boilerplate 標準）, `stretchr/testify`。**Python は削除**

### テスト: `go test` + testify

- unit 相当（`internal/cli`）と、fake `codex` を生成する end-to-end 相当（`cmd/root_test.go`、`internal/runner/runner_test.go`）を混在させる
- fake `codex` は `os.WriteFile` で POSIX sh script を temp dir に書き、`CODEX_BIN` env で差し込む（zsh 版の `make_fake_codex` を Go 側で再実装）
- fixture jsonl は `internal/usage/testdata/` 配下（zsh 版 `tests/fixtures/` から移設）

### Makefile: boilerplate 準拠 + install/uninstall を維持

- `deps / build / lint / test / test-cov / check / ci / clean / help` は boilerplate から採用
- v0.1.0 で提供していた `install` / `uninstall` の PREFIX ベースのシンプルな UX を維持（`make install PREFIX=/usr/local` など）。boilerplate 既定の `install`（`go mod download`）は `deps` にリネームして共存
- `VERSION` 変数を ldflags 経由で `cmd.Version` に注入（release tag と一致させる運用）

### CI: `setup-go@v6` + `golangci-lint-action@v9` + `go test -cover`（80% gate）

- zsh / python3 の apt install を削除
- Coverage gate はテンプレ既定の 80%（現状 86.5%）
- lint: golangci-lint v2、errcheck の `fmt.Fprint*` 除外、gocritic の `hugeParam` 除外を .golangci.yml で明示

## Consequences

### Pros

- ランタイム依存が `codex` のみ（zsh / python3 / GNU timeout が不要）
- `go install github.com/rengotaku/codex-run@latest` で入る（README に記載）
- rollout jsonl parser が型検査され、schema 変更に対する耐性が上がる
- boilerplate の CI / lint / test / release レール（enh の受け皿・PR テンプレ・issue テンプレ）にそのまま乗る
- 後続機能（並列化・JSON 出力・env 経由 regex 差し込み）を素直に足せる

### Cons

- ビルドに Go toolchain（1.24+）が要る（`go install` を使わず shell 経由で入れたい人にとっては前より重い）
- テストが zsh 版より複雑化（fake codex を Go 側で生成 → `os.WriteFile` + `CODEX_BIN` 経由）
- 単一 zsh ファイルに比べ「読んで理解する量」は増える（`cmd/` + `internal/*` の 4 パッケージ）

### 互換性

- CLI 契約は v0.1.0 と完全互換:
  - stdout: 成功時に絶対パス 1 行
  - stderr: 進捗ログ + attempts + usage 1 行
  - exit: 成功 0 / 全滅時は最後の exit code（timeout は 124）
  - passthrough: 未知フラグ（`--skip-git-repo-check` 等）を `codex exec` に転送
- 環境変数: `CODEX_MODELS`, `CODEX_TIMEOUT`, `CODEX_BIN`, `CODEX_SESSIONS_DIR`, `XDG_STATE_HOME` はそのまま
- usage.log の TSV フォーマット（`ts plan used_pct window_min tokens_in tokens_out model exit`）は unchanged
- 旧 zsh 実装は `git checkout v0.1.0` で復元可能

## Alternatives Considered

- **zsh のまま拡張を続ける（ADR 0001 継続）** — ubiterm.my-boilerplate-first 違反が続き、Go 化コストは backlog を消化する頃には更に膨らむ。今 payoff の低いうちに移す方が総コストが小さい
- **段階的移行（zsh を残して Go 実装を並置し、旧 CLI が新 CLI を呼ぶ）** — 一時的に2実装を持つ手間の割にメリットが薄い。v0.1.0 tag が rollback 経路として十分機能する
- **Rust / TypeScript** — boilerplate が Go を第一に整備しており、姉妹の `agy-run` も Go。Go を選ぶことでレール共有・レビュー観点共有・skill 再利用のメリットが最大化する

## Trigger to revisit

- 並列モデル呼び出し / 有界並列 / クールダウンを実装する必要が出たら → runner を interface 化し `internal/orchestrator` を導入（本 ADR のまま）
- `agy-run` と機能を共通化する規模になったら → 共通ライブラリ抽出を検討（本 ADR のまま）
- Go の運用が破綻するような制約（例: 実行環境が Go を持てない超軽量 CI）が出たら → 本 ADR を supersede する新 ADR を書く
