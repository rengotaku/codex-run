#!/bin/zsh
source "${0:A:h}/lib.zsh"

print -- "# test_passthrough_and_out"

# --out FILE: first attempt writes to FILE
FAKE=$(mktemp -d)
make_fake_codex "$FAKE" "ok"
OUT=$(mktemp)
run_hermetic "$FAKE" --timeout 5 --models m1 --out "$OUT" "hi"
assert_eq "--out succeeds" 0 $RC_EXIT
assert_eq "stdout is the --out path" "$OUT" "$RC_STDOUT"
assert_file_contains "--out file has content" "reply" "$OUT"

# Pass-through args reach codex exec verbatim
FAKE2=$(mktemp -d)
make_fake_codex "$FAKE2" "echo-argv"
run_hermetic "$FAKE2" --timeout 5 --models mX -c foo=bar --skip-git-repo-check "hello world"
assert_eq "pass-through exit 0" 0 $RC_EXIT
argv=$(<"$RC_STDOUT")
assert_match "codex received -m" "-m mX" "$argv"
assert_match "codex received -c foo=bar" "-c foo=bar" "$argv"
assert_match "codex received --skip-git-repo-check" "--skip-git-repo-check" "$argv"
assert_match "codex received PROMPT" "hello world" "$argv"

# `--` separator forces everything after to be pass-through + trailing PROMPT
FAKE3=$(mktemp -d)
make_fake_codex "$FAKE3" "echo-argv"
run_hermetic "$FAKE3" --timeout 5 --models mY -- --skip-git-repo-check "the prompt"
argv3=$(<"$RC_STDOUT")
assert_eq "-- separator: exit 0" 0 $RC_EXIT
assert_match "-- separator: forwards flag" "--skip-git-repo-check" "$argv3"
assert_match "-- separator: last positional is PROMPT" "the prompt" "$argv3"

test_summary test_passthrough_and_out
