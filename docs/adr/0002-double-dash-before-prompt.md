# ADR 0002: `codex exec` 呼び出し時に `--` を PROMPT の直前に挿入する

## Status

Accepted (v0.1.0)

## Context

`bin/codex-run` は次の形で `codex exec` を呼ぶ:

```zsh
timeout "${TIMEOUT_SEC}" "$CODEX_BIN" exec -m "$MODEL" "${PASSTHROUGH[@]}" -- "$PROMPT" >"$out_path" 2>&1
```

`PASSTHROUGH` はユーザーが与えた未知フラグ（`-c foo=bar`, `--skip-git-repo-check` 等）で任意の順序・任意の数。PROMPT は最後の positional。

もし `PROMPT` が `-` で始まる（例: `-x とは何？`）と、`codex exec` の clap パーサは PROMPT をフラグとして解釈しようとして失敗するリスクがある。

## Decision

**常に `--` を PROMPT の直前に挿入する**。

## Consequences

### Pros

- PROMPT が任意の文字列で始まっても安全（`--`, `-`, `--models=...` 相当の文字列でも壊れない）
- pass-through の順序・数を気にせずに書ける

### Cons

- ごく稀に、`codex exec` の将来バージョンで `--` を positional として扱う変更が入ると壊れる（現時点の codex 0.144.x では標準的な clap 挙動で問題なし。tests/test_passthrough_and_out.zsh の `--` セパレータテストが回帰検知になる）

## Alternatives Considered

- **`--` を挿入しない**: pass-through の中に `--` が既にある場合の二重挿入が問題になり得るが、それは実質的に稀。ただし PROMPT の先頭 `-` を許容できない
- **PROMPT を stdin で渡す**: `codex exec` は `-` または stdin から PROMPT を読める仕様。将来 PROMPT が長大化した場合はこの経路を検討する価値がある（引数長制限が問題になったら ADR 0003 で切り替え）

## Trigger to revisit

- codex CLI の argv 契約が変わったとき
- PROMPT の平均サイズが `ARG_MAX` に近づいたとき
- ユーザーが `--` を含む pass-through を渡すユースケースが実際に発生したとき
