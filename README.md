# codex-run

`codex exec` の薄いラッパー CLI。姉妹プロジェクトの [rengotaku/agy-run](https://github.com/rengotaku/agy-run)（Antigravity/Gemini CLI ラッパー）と同じ位置付け。

生の `codex exec` を叩かず必ず `codex-run` を経由することで、次を機械的に担保する。

- モデル capacity error / 429 / rate limit / quota exceeded を検知し、宣言済みの fallback chain に沿って自動リトライ
- 出力は常に `mktemp` のユニーク名にリダイレクトされ、並行実行のレースで上書きされない
- 各モデルの実行に強制 timeout（デフォ 600s）
- 失敗 trace ファイルは削除しない
- 実行後に週次 rate limit / トークン使用量を stderr サマリ + `usage.log` へ記録

## Install

```bash
git clone git@github.com:rengotaku/codex-run.git
cd codex-run
make install                     # symlinks bin/codex-run -> ~/.local/bin/codex-run
# もしくは
make install PREFIX=/usr/local   # /usr/local/bin/codex-run
```

Uninstall:

```bash
make uninstall            # or: make uninstall PREFIX=/usr/local
```

依存: `zsh`, `python3`, GNU `timeout`（`coreutils`）, `codex`（OpenAI Codex CLI）

## Usage

```
codex-run [OPTIONS] [codex-exec-args...] PROMPT
```

### Options

| Option | Description |
|---|---|
| `--models a,b,c` | comma-separated モデル fallback chain（env: `CODEX_MODELS`） |
| `--out FILE` | attempt #1 の出力を FILE へ書き込む（retry は常に `mktemp`） |
| `--timeout SEC` | モデル 1 回あたりの timeout 秒数（env: `CODEX_TIMEOUT`, 既定 600） |
| `-h`, `--help` | ヘルプ |
| `--version` | バージョン |

上記以外のフラグ（例: `--skip-git-repo-check`, `-c key=val`）は `codex exec` へそのまま渡す。**PROMPT は常に最後の positional 引数**。曖昧なら `--` セパレータを使う。

### CLI 契約

- **stdout**: 成功時、成功した attempt の出力ファイルの絶対パスを 1 行のみ。`out=$(codex-run "...")` で使える
- **stderr**: 進捗ログ・attempts サマリ・usage 1 行
- **exit**: 成功時 0、全モデル失敗時は最後に試したモデルの exit code

### 例

```bash
# 基本
codex-run "この設計をレビューして"

# fallback chain を明示
codex-run --models gpt-5.6-luna,gpt-5.5 "..."

# codex exec の pass-through
codex-run --skip-git-repo-check "$(cat brief.md)"

# 出力ファイルを使う
out=$(codex-run "設計レビュー: $(cat design.md)") && less "$out"
```

## 環境変数

| Env | 説明 |
|---|---|
| `CODEX_MODELS` | fallback chain の既定値（例: `gpt-5.6-sol,gpt-5.6-luna,gpt-5.6-terra,gpt-5.5`） |
| `CODEX_TIMEOUT` | モデル 1 回の timeout 秒数（既定 600） |
| `CODEX_BIN` | `codex` バイナリのパス（既定 `codex`。テスト用） |
| `CODEX_SESSIONS_DIR` | rollout jsonl の探索ルート（既定 `~/.codex/sessions`） |
| `XDG_STATE_HOME` | `usage.log` の格納先（既定 `~/.local/state`。実ファイルは `<XDG_STATE_HOME>/codex-run/usage.log`） |

## usage.log の形式

TSV（1 行目はヘッダ）:

```
ts	plan	used_pct	window_min	tokens_in	tokens_out	model	exit
2026-07-21T03:00:41+0900	plus	3.5	10080	1500	350	gpt-5.6-sol	0
```

- `used_pct` / `window_min` は `~/.codex/sessions/**/rollout-*.jsonl` の最後の `rate_limits.primary` から取得
- `tokens_in` / `tokens_out` は同じ jsonl の最後の `token_count.info.total_token_usage` から取得
- 取得できないケース（rollout jsonl が無い・壊れている等）は `-` を書き込み、silent skip warning を stderr に 1 行だけ出す（exit code に影響しない）

## Fallback chain の挙動

1. `--models` / `CODEX_MODELS` を comma で分割
2. 先頭モデルから順に `codex exec -m <model> <passthrough...> -- <PROMPT>` を実行
3. 次のいずれかで**リトライ**（次モデルへ）:
   - exit code が 0 でない
   - exit code が 124（GNU timeout）
   - 出力に `Selected model is at capacity` / `429` / `rate.?limit` / `quota exceeded` / `Too Many Requests` を検知
4. どこかで**成功したら**、そのパスを stdout に 1 行出して exit 0
5. **全モデル失敗**したら、最後に試したモデルの exit code をそのまま返す

各 attempt の出力ファイルは削除されない（失敗 trace の保全）。ファイル名は `mktemp -t "codex-run-<model>-XXXXXX"` パターン。

## Development

```bash
make lint       # zsh -n
make test       # tests/run.zsh (hermetic; ~/.codex は触らない)
make ci         # lint + test
```

テストは `tests/lib.zsh` に fake codex ジェネレータと assert helper を集約し、`tests/test_*.zsh` は plain zsh。bats などの追加依存は導入していない（MVP 方針）。

## ubiterm.codex との関係

`rengotaku/dotfiles-ubuntu` の `linux/claude/skills/ubiterm.codex/SKILL.md` の「codex exec 直接使用禁止」ルールは、この `codex-run` の存在を前提とする。Claude / 人間は常にこの CLI 経由で codex を呼ぶこと。dotfiles 側の統合（`codex exec` 直呼び検知 hook, `ubiterm.codex` の更新）は別リポの issue で管理する（このリポの scope 外）。

## License

MIT
