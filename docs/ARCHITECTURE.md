# codex-run Architecture

`codex exec` の薄いラッパー CLI。Go の単一バイナリで完結する（v0.2.0〜）。旧 zsh 実装は tag `v0.1.0` 参照。

## 全体像

```
codex-run (呼び出し側)
    │
    ▼
main → cmd.Execute() → cobra rootCmd (DisableFlagParsing)
                       │
                       ▼
                   internal/cli.Parse(argv)      // 独自パーサ: passthrough 保持
                       │
                       ▼
                   internal/runner.Run(ctx, cfg)  // fallback loop
                       ├─ 各モデルで:
                       │    ├─ chooseOutPath: --out(#1のみ) or os.CreateTemp
                       │    ├─ exec.CommandContext(ctx, codex, "exec", "-m", MODEL, ...passthrough..., "--", PROMPT)
                       │    ├─ context.DeadlineExceeded → exit 124（GNU timeout 互換）
                       │    ├─ 出力を regexp で検査（capacity/429/rate limit/quota）
                       │    └─ 成功なら break、失敗なら次モデル
                       └─ Reporter(startTS, model, exitCode)
                           │
                           ▼
                       internal/usage.Report()   // rollout jsonl → stderr + usage.log TSV
```

## ディレクトリ

| Path | 役割 |
|---|---|
| `main.go` | `os.Exit(cmd.Execute())` のみ |
| `cmd/root.go` | Cobra root（`DisableFlagParsing: true`）、env 解決、runner/usage 呼び出し |
| `cmd/root_test.go` | エンドツーエンド（fake codex を fs 上に生成して subprocess 実行） |
| `internal/cli/parse.go` | 引数パース。未知フラグは positional として保持し `codex exec` に転送 |
| `internal/runner/runner.go` | fallback loop、timeout、mktemp、FAIL_PATTERNS |
| `internal/usage/usage.go` | rollout jsonl の最新特定・parse・TSV 追記・silent skip |
| `internal/usage/testdata/` | fixture jsonl |
| `Makefile` | `deps / build / lint / test / test-cov / ci / install / uninstall / clean / help` |
| `.golangci.yml` | lint 設定（errcheck の fmt.Fprint* 除外、gocritic の hugeParam 除外） |
| `.github/workflows/ci.yml` | GitHub Actions（`setup-go@v6` + `golangci-lint-action@v9` + `go test -cover`（80% gate）） |
| `docs/ARCHITECTURE.md` | 本ファイル |
| `docs/adr/` | 設計判断の記録（ADR） |

## データフロー

### 入力

- 引数: `[OPTIONS] [codex-exec-args...] PROMPT`
  - PROMPT は**常に最後の positional**
  - `--` セパレータで pass-through と PROMPT の境界を明示可能
  - 未知フラグ（`--skip-git-repo-check` 等）は positional として `codex exec` に転送
- 環境変数: `CODEX_MODELS / CODEX_TIMEOUT / CODEX_BIN / CODEX_SESSIONS_DIR / XDG_STATE_HOME`

### 出力

| Stream | 内容 |
|---|---|
| stdout | 成功時のみ、成功 attempt の出力ファイルの絶対パスを 1 行 |
| stderr | 進捗ログ・attempts サマリ・usage 1 行・（あれば）silent skip warning |
| exit | 成功 0 / 全モデル失敗時は最後の attempt の exit code（timeout は 124） |
| ファイル | `os.TempDir()/codex-run-<model>-XXXXXX`（各 attempt の生 codex 出力。失敗 trace も残す） |
| ファイル | `$XDG_STATE_HOME/codex-run/usage.log`（TSV、ヘッダ + 追記） |

## 「どこを触れば何が変わるか」

- **fallback chain の regex 変更** → `internal/runner/runner.go` の `failPatterns`
- **既定モデル追加/変更** → `cmd/root.go` の `DefaultModels`
- **timeout 既定値** → `cmd/root.go` の `DefaultTimeoutSec`
- **usage 抽出のフィールド追加** → `internal/usage/usage.go` の `parseRollout()` の JSON struct と `Summary` 型
- **usage.log の TSV 列追加** → `internal/usage/usage.go` の `appendTSVRow()` のヘッダ行と row 生成部を同時更新（README の説明も）
- **CLI 引数追加** → `internal/cli/parse.go` の `Parse()` に case を追加 + `cmd/root.go` の `usageText` を更新
- **新しいテスト** → 対応する `*_test.go` に追加（testify assert/require）。fake codex は `cmd/root_test.go` の `writeFakeCodex()` パターンで sh script を temp に置く

## 変えてよい前提／壊すと危ない前提

- ✅ 変えてよい: `DefaultModels` / `DefaultTimeoutSec` / `failPatterns` の regex / usage.log の TSV 列
- ⚠️ 壊すと危ない:
  - **stdout に成功パス 1 行のみ**（`out=$(codex-run "...")` パターンが依存）
  - **失敗 trace ファイルを削除しない**（デバッグの根拠を保全）
  - **`usage.Report` は絶対に exit code を変えない**（silent skip 原則。全ての error は stderr 1 行に落とす）
  - **並行実行の race に対する耐性**（`os.CreateTemp` のユニーク名。固定名 out へ回帰しない）
  - **`--out FILE` は attempt #1 のみ**（retry は必ず mktemp。CLI 契約）
  - **未知フラグの passthrough**（`DisableFlagParsing: true` を外すと `--skip-git-repo-check` 等が「unknown flag」として拒否され、v0.1.0 との互換が崩れる。ADR 0002 と対）

## 拡張の指針

- **session-id ベース rollout 特定**（#10） → `codex exec` の stderr / stdout から session id を capture し、`internal/usage` に `pathBySessionID()` を追加
- **`--format json` 出力**（#11） → `cmd/root.go` に `--format` を追加、runner の stdout 契約（成功パス 1 行）を JSON に切り替え可能に。既定は現行契約を維持
- **`CODEX_FAIL_PATTERNS` env**（#12） → `internal/runner/runner.go` の `failPatterns` を Config 経由で差し込み可能に
- **並列モデル呼び出し / キューイング** → runner を interface 化し、`internal/orchestrator` を新設。今の直列 loop は default 実装として残す
