#!/bin/zsh
source "${0:A:h}/lib.zsh"

print -- "# test_usage"

# Set up a sessions dir containing the fixture jsonl at a timestamped path.
FIX_DIR=$(mktemp -d)
mkdir -p "$FIX_DIR/2026/07/21"
cp "${0:A:h}/fixtures/rollout-two-events.jsonl" \
   "$FIX_DIR/2026/07/21/rollout-2026-07-21T02-58-37-fake.jsonl"

FAKE=$(mktemp -d)
make_fake_codex "$FAKE" "ok"

RC_SESSIONS_OVERRIDE="$FIX_DIR" run_hermetic "$FAKE" --timeout 5 --models m1 "hi"
assert_eq "usage: exit 0" 0 $RC_EXIT

# Latest event (used=3.5) must win, not the earlier 3.0
assert_match "stderr shows last used_percent" "used=3.5%" "$RC_STDERR"
assert_match "stderr shows plan=plus" "plan=plus" "$RC_STDERR"
assert_match "stderr shows tokens" "tokens=in1500/out350" "$RC_STDERR"

# usage.log TSV: header + 1 row
assert_file_contains "usage.log has header" "used_pct" "$RC_USAGE_LOG"
assert_file_contains "usage.log row has plan" "plus" "$RC_USAGE_LOG"
assert_file_contains "usage.log row has model m1" "m1" "$RC_USAGE_LOG"
row_count=$(wc -l < "$RC_USAGE_LOG" | tr -d ' ')
assert_eq "usage.log has header + 1 data row" 2 "$row_count"

# Empty sessions dir -> silent skip, exit unchanged
EMPTY=$(mktemp -d)
RC_SESSIONS_OVERRIDE="$EMPTY" run_hermetic "$FAKE" --timeout 5 --models m1 "hi"
assert_eq "empty sessions: exit unchanged" 0 $RC_EXIT
assert_match "empty sessions: warning to stderr" "no-fresh-rollout" "$RC_STDERR"

test_summary test_usage
