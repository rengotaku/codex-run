# ADR 0001: MVP を単一 zsh ファイル + plain zsh テストで構成する

## Status

Accepted (v0.1.0)

## Context

`codex-run` は `codex exec` の薄いラッパーで、機能は次の 4 系統に集約される:

1. モデル fallback（chain を舐めながら retry）
2. mktemp によるユニーク名出力
3. 強制 timeout
4. 実行後の rate_limits / token usage 抽出

姉妹プロジェクト `agy-run` は Go で書かれているが、あちらは**有界並列・強制タイムアウト・クールダウン**等で複雑度が耐えられなくなった経緯がある。`codex-run` は当面それらの並行制御を持たない。

またテスト規模は現在 4 ファイル・37 アサーション。bats / shellspec の追加依存を要求するほどのボリュームはない。

## Decision

### 実装は zsh 1 ファイル（`bin/codex-run`）

- ロジックの本体は `subprocess + テキスト検査 + JSON parse`
- JSON parse は inline `python3 - <<PY` heredoc（Python は依存として既に許容範囲）
- 依存: `zsh, python3, GNU timeout, codex`（すべて OS 標準または既存の CLI 環境）
- インストールは `make install`（`bin/codex-run` を `$PREFIX/bin` に symlink）

### テストは plain zsh + 自作 harness（`tests/lib.zsh`）

- assert helper（`assert_eq / assert_match / assert_file_contains`）は 10 行
- fake codex generator（`make_fake_codex`）は spec 文字列でモード切替
- hermetic ランナー（`run_hermetic`）が `~/.codex` を触らないよう `CODEX_BIN / XDG_STATE_HOME / CODEX_SESSIONS_DIR` を隔離
- `tests/run.zsh` が `test_*.zsh` を自動収集

### CI は GitHub Actions で `apt install zsh coreutils python3 → make ci`

- ubuntu-latest には zsh が未インストールなので明示 install（本 repo の CI 定義参照）

## Consequences

### Pros

- 開発マシンで `git clone && make ci` が即動く（追加ツール不要）
- CI ワークフローが 12 行に収まる
- 新規テストは `tests/test_*.zsh` を 1 ファイル増やすだけ（`tests/run.zsh` が自動で拾う）
- README / Makefile / テストがすべて 1 リポジトリ内で自己完結

### Cons

- テストランナーの機能（parallel, timeout per test, TAP 準拠等）は自前実装が必要になる
- 複雑な文字列比較・fixture 管理が増えると bats の恩恵が出る可能性
- 純粋な zsh 依存のため CI で `apt install zsh` が要る（trivial だが一手間）

## Alternatives Considered

- **Go 実装**: agy-run 準拠。だが並列/クールダウン等の必要性が無く、現在のスコープではオーバースペック。将来複雑度が上がった時点で ADR 0002 で再検討する
- **bats-core 導入**: `git submodule` で vendoring するか apt install する。テスト規模が数倍になった段階で再検討
- **shellspec**: bats より重い依存

## Trigger to revisit

- ソース行数が 400 行を超えたら
- テスト数が 100 アサーションを超えたら
- 並列モデル呼び出し・キューイング等、control-plane 相当の機能を実装するとき
- rollout jsonl schema が破壊的に変わり Python heredoc が肥大化したら
