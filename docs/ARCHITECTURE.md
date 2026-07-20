# codex-run Architecture

`codex exec` の薄いラッパー CLI。単一 zsh ファイル + 補助ドキュメントで完結する。

## 全体像

```
codex-run (呼び出し側)
    │
    ▼
bin/codex-run              [zsh, 単一ファイル]
  ├─ arg parse             （--models / --out / --timeout / -- / pass-through）
  ├─ fallback loop         各モデルで:
  │    ├─ mktemp で out_path 決定
  │    ├─ timeout SEC codex exec -m MODEL <passthrough> -- <PROMPT> > out_path 2>&1
  │    ├─ exit / 出力から capacity/rate/quota/timeout を判定
  │    └─ 成功なら break、失敗なら次モデルへ
  ├─ attempts サマリ       stderr
  ├─ report_usage()        Python inline heredoc:
  │    ├─ ~/.codex/sessions/**/rollout-*.jsonl から
  │    │  RUN_START_TS 以降の最新を特定
  │    ├─ 最後の token_count/rate_limits を抽出
  │    ├─ stderr に 1 行サマリ
  │    └─ $XDG_STATE_HOME/codex-run/usage.log に TSV 追記
  └─ stdout: 成功パス 1 行 / exit: 最後の attempt の code
```

## ディレクトリ

| Path | 役割 |
|---|---|
| `bin/codex-run` | 本体。zsh 1 ファイル |
| `tests/lib.zsh` | assert helper + fake codex generator + hermetic ランナー |
| `tests/run.zsh` | `test_*.zsh` を全実行するテストランナー |
| `tests/test_*.zsh` | ケース別テストスクリプト |
| `tests/fixtures/` | rollout jsonl 等のテストデータ |
| `Makefile` | `lint / test / ci / install / uninstall` |
| `.github/workflows/ci.yml` | GitHub Actions（apt install zsh coreutils python3 → `make ci`） |
| `docs/ARCHITECTURE.md` | 本ファイル |
| `docs/adr/` | 設計判断の記録（ADR） |

## データフロー

### 入力

- 引数: `[OPTIONS] [codex-exec-args...] PROMPT`
  - PROMPT は**常に最後の positional**
  - `--` セパレータで pass-through と PROMPT の境界を明示可能
- 環境変数: `CODEX_MODELS / CODEX_TIMEOUT / CODEX_BIN / CODEX_SESSIONS_DIR / XDG_STATE_HOME`

### 出力

| Stream | 内容 |
|---|---|
| stdout | 成功時のみ、成功 attempt の出力ファイルの絶対パスを 1 行 |
| stderr | 進捗ログ・attempts サマリ・usage 1 行・（あれば）silent skip warning |
| exit | 成功 0 / 全モデル失敗時は最後の attempt の exit code（timeout は 124） |
| ファイル | `/tmp/codex-run-<model>-XXXXXX`（各 attempt の生 codex 出力。失敗 trace も残す） |
| ファイル | `$XDG_STATE_HOME/codex-run/usage.log`（TSV、ヘッダ + 追記） |

## 「どこを触れば何が変わるか」

- **fallback chain のパターン変更** → `bin/codex-run` の `FAIL_PATTERNS` 変数
- **既定モデル追加/変更** → `bin/codex-run` の `DEFAULT_MODELS`
- **timeout 既定値** → `bin/codex-run` の `DEFAULT_TIMEOUT`
- **usage 抽出のフィールド追加** → `bin/codex-run` の `report_usage()` 内 Python heredoc
- **usage.log の TSV 列追加** → 上記 heredoc の `row = ...` 行 + `header` 行を同時更新（README のフォーマット表も）
- **新しいテスト** → `tests/test_<topic>.zsh` を追加（`tests/run.zsh` が自動で拾う）。fake codex spec を追加したい場合は `tests/lib.zsh` の `make_fake_codex()` に case を追加

## 変えてよい前提／壊すと危ない前提

- ✅ 変えてよい: `DEFAULT_MODELS` / `DEFAULT_TIMEOUT` / `FAIL_PATTERNS` の regex / usage.log の TSV 列
- ⚠️ 壊すと危ない:
  - **stdout に成功パス 1 行のみ**（`out=$(codex-run "...")` パターンが依存）
  - **失敗 trace ファイルを削除しない**（デバッグの根拠を保全）
  - **`report_usage` は絶対に exit code を変えない**（silent skip 原則）
  - **並行実行の race に対する耐性**（`mktemp` ユニーク名。固定名 out へ回帰しない）
  - **`--out FILE` は attempt #1 のみ**（retry は必ず mktemp。仕様上 `codex-run` の契約）

## 拡張の指針

- 実装が 400 行を超える or 並列モデル呼び出し等の複雑度が入る → Go 化を検討（姉妹の agy-run が既にこのパスを通っている）
- 新しい出力フォーマット（JSON 契約等）が必要 → `--format json` 相当のオプションを追加、既存 stdout 契約は互換維持
- codex 側の rollout jsonl schema が変わった → `report_usage()` の Python heredoc を差し替え、fixture jsonl を新形式で更新して回帰テスト
