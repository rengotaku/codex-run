#!/bin/zsh
# tiny test harness shared by tests/*.zsh — no external deps.

REPO_ROOT="${${(%):-%N}:A:h:h}"
CODEX_RUN="$REPO_ROOT/bin/codex-run"

typeset -g TEST_PASS=0 TEST_FAIL=0
typeset -ga TEST_FAILURES
TEST_FAILURES=()

pass() { print -- "  ok - $1"; ((TEST_PASS++)); }
fail() {
  print -u2 -- "  not ok - $1"
  print -u2 -- "    $2"
  TEST_FAILURES+=("$1: $2")
  ((TEST_FAIL++))
}

assert_eq() {
  local label="$1" want="$2" got="$3"
  if [[ "$want" == "$got" ]]; then pass "$label"
  else fail "$label" "want=<$want> got=<$got>"
  fi
}

assert_match() {
  local label="$1" pat="$2" got="$3"
  if [[ "$got" == *"$pat"* ]]; then pass "$label"
  else fail "$label" "expected <$got> to contain <$pat>"
  fi
}

assert_file_contains() {
  local label="$1" pat="$2" file="$3"
  if [[ -f "$file" ]] && grep -qF -- "$pat" "$file"; then pass "$label"
  else fail "$label" "file '$file' missing pattern <$pat>"
  fi
}

# make_fake_codex <dir> <spec>
# spec: one of
#   ok                              -> always exit 0, prints "reply"
#   capacity-then-ok:<model>        -> exits with capacity error unless -m is <model>
#   sleep:<sec>                     -> sleeps N seconds then exits 0
#   fail-429                        -> prints "429 Too Many Requests" and exits 3
#   echo-argv                       -> prints "ARGV:<all args>" and exits 0
make_fake_codex() {
  local dir="$1" spec="$2"
  mkdir -p "$dir"
  local script="$dir/codex"
  case "$spec" in
    ok)
      cat > "$script" <<'FAKE'
#!/bin/bash
echo "reply"
FAKE
      ;;
    fail-429)
      cat > "$script" <<'FAKE'
#!/bin/bash
echo "429 Too Many Requests" >&2
exit 3
FAKE
      ;;
    echo-argv)
      cat > "$script" <<'FAKE'
#!/bin/bash
echo "ARGV:$*"
FAKE
      ;;
    sleep:*)
      local sec="${spec#sleep:}"
      cat > "$script" <<FAKE
#!/bin/bash
sleep $sec
FAKE
      ;;
    capacity-then-ok:*)
      local ok_model="${spec#capacity-then-ok:}"
      cat > "$script" <<FAKE
#!/bin/bash
model=""
for ((i=1; i<=\$#; i++)); do
  if [[ "\${!i}" == "-m" ]]; then j=\$((i+1)); model="\${!j}"; break; fi
done
if [[ "\$model" != "$ok_model" ]]; then
  echo "ERROR: Selected model is at capacity. Please try a different model." >&2
  exit 1
fi
echo "reply from \$model"
FAKE
      ;;
    *) print -u2 "make_fake_codex: unknown spec: $spec"; return 1 ;;
  esac
  chmod +x "$script"
}

# run codex-run in a fully hermetic env (no real ~/.codex touched).
# Prints its stdout; stderr and exit are captured for later inspection.
# Usage: run_hermetic <fake_dir> [args...]
#   sets globals: RC_STDOUT, RC_STDERR, RC_EXIT, RC_USAGE_LOG, RC_SESSIONS
run_hermetic() {
  local fake="$1"; shift
  RC_STATE="$(mktemp -d)"
  RC_SESSIONS="${RC_SESSIONS_OVERRIDE:-$(mktemp -d)}"
  RC_USAGE_LOG="$RC_STATE/codex-run/usage.log"
  local out_file err_file
  out_file="$(mktemp)"
  err_file="$(mktemp)"
  CODEX_BIN="$fake/codex" \
    XDG_STATE_HOME="$RC_STATE" \
    CODEX_SESSIONS_DIR="$RC_SESSIONS" \
    "$CODEX_RUN" "$@" >"$out_file" 2>"$err_file"
  RC_EXIT=$?
  RC_STDOUT="$(<"$out_file")"
  RC_STDERR="$(<"$err_file")"
  rm -f "$out_file" "$err_file"
}

test_summary() {
  local name="$1"
  print -- ""
  print -- "# $name: pass=$TEST_PASS fail=$TEST_FAIL"
  if (( TEST_FAIL > 0 )); then
    print -u2 -- "FAILED:"
    for f in "${TEST_FAILURES[@]}"; do print -u2 -- "  - $f"; done
    exit 1
  fi
}
