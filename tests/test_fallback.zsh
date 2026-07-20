#!/bin/zsh
source "${0:A:h}/lib.zsh"

print -- "# test_fallback"

# Case 1: first model returns capacity, second succeeds -> exit 0, stdout is the second attempt's path
FAKE=$(mktemp -d)
make_fake_codex "$FAKE" "capacity-then-ok:model-b"
run_hermetic "$FAKE" --timeout 5 --models model-a,model-b "hello"
assert_eq "capacity -> next model succeeds: exit 0" 0 $RC_EXIT
assert_match "stdout is single line path" "/" "$RC_STDOUT"
# stdout must contain exactly one newline (from `print`) -> lines count == 1
line_count=$(print -r -- "$RC_STDOUT" | wc -l | tr -d ' ')
assert_eq "stdout is single line" 1 "$line_count"
assert_file_contains "success attempt file has content" "reply from model-b" "$RC_STDOUT"
assert_match "stderr logs both attempts" "attempts:" "$RC_STDERR"
assert_match "stderr flags capacity failure" "fail-pattern" "$RC_STDERR"

# Case 2: all models fail -> exit == last model's exit code
FAKE2=$(mktemp -d)
make_fake_codex "$FAKE2" "fail-429"
run_hermetic "$FAKE2" --timeout 2 --models a,b,c "x"
assert_eq "all-fail preserves last exit code" 3 $RC_EXIT
assert_eq "all-fail produces no stdout" "" "$RC_STDOUT"

# Case 3: timeout triggers fallback
FAKE3=$(mktemp -d)
make_fake_codex "$FAKE3" "sleep:10"
run_hermetic "$FAKE3" --timeout 1 --models slow-a,slow-b "x"
# Both models will timeout -> exit 124
assert_eq "timeout exit 124" 124 $RC_EXIT
assert_match "stderr reports timeout" "timeout after 1s" "$RC_STDERR"

test_summary test_fallback
