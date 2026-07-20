#!/bin/zsh
source "${0:A:h}/lib.zsh"

print -- "# test_help_and_errors"

# --help exits 0
"$CODEX_RUN" --help >/dev/null 2>&1
assert_eq "--help exits 0" 0 $?

# --version prints codex-run x.y.z
v=$("$CODEX_RUN" --version)
assert_match "--version prints version" "codex-run " "$v"

# No PROMPT -> exit 2
"$CODEX_RUN" >/dev/null 2>&1
assert_eq "no args exits 2" 2 $?

# --timeout with non-numeric -> exit 2
"$CODEX_RUN" --timeout abc "x" >/dev/null 2>&1
assert_eq "--timeout abc exits 2" 2 $?

# --models with empty chain -> exit 2
"$CODEX_RUN" --models "" "x" >/dev/null 2>&1
assert_eq "--models '' exits 2" 2 $?

# missing CODEX_BIN -> exit 127
CODEX_BIN=/does/not/exist "$CODEX_RUN" --models m1 "x" >/dev/null 2>&1
assert_eq "missing codex bin exits 127" 127 $?

test_summary test_help_and_errors
