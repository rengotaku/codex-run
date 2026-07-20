#!/bin/zsh
# Run every tests/test_*.zsh in a subshell and report pass/fail totals.
set -u
DIR="${0:A:h}"
total_pass=0 total_fail=0 files_fail=0
for t in "$DIR"/test_*.zsh; do
  [[ -e "$t" ]] || continue
  print -- "==> $(basename $t)"
  if zsh "$t"; then :; else ((files_fail++)); fi
done
print -- ""
if (( files_fail > 0 )); then
  print -u2 -- "FAIL: $files_fail test file(s) failed"
  exit 1
fi
print -- "OK: all test files passed"
