package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFakeCodex(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

// isolateEnv resets every codex-run env var so the current shell's real
// $CODEX_MODELS etc. can't leak into the CLI test.
func isolateEnv(t *testing.T, codexBin, sessionsDir, stateHome string) {
	t.Helper()
	t.Setenv("CODEX_BIN", codexBin)
	t.Setenv("CODEX_MODELS", "")
	t.Setenv("CODEX_TIMEOUT", "")
	t.Setenv("CODEX_SESSIONS_DIR", sessionsDir)
	t.Setenv("XDG_STATE_HOME", stateHome)
}

func TestRun_help(t *testing.T) {
	isolateEnv(t, "codex", t.TempDir(), t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--help"}, &stdout, &stderr)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), "Usage:")
	assert.Contains(t, stdout.String(), "--models")
}

func TestRun_version(t *testing.T) {
	isolateEnv(t, "codex", t.TempDir(), t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--version"}, &stdout, &stderr)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), "codex-run ")
}

func TestRun_missingPrompt(t *testing.T) {
	isolateEnv(t, "codex", t.TempDir(), t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "PROMPT is required")
}

func TestRun_invalidTimeout(t *testing.T) {
	isolateEnv(t, "codex", t.TempDir(), t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--timeout", "0", "p"}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "positive integer")
}

func TestRun_endToEndSuccess(t *testing.T) {
	fake := writeFakeCodex(t, `#!/bin/sh
echo "hello from fake"
exit 0
`)
	sess := t.TempDir()
	state := t.TempDir()
	isolateEnv(t, fake, sess, state)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(),
		[]string{"--models", "m1", "--timeout", "5", "hello"},
		&stdout, &stderr)
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "[attempt 1/1] model=m1")
	success := strings.TrimSpace(stdout.String())
	require.NotEmpty(t, success)
	content, err := os.ReadFile(success)
	require.NoError(t, err)
	assert.Contains(t, string(content), "hello from fake")
}

func TestRun_envDefaultsHonored(t *testing.T) {
	fake := writeFakeCodex(t, `#!/bin/sh
echo "ok"
exit 0
`)
	sess := t.TempDir()
	state := t.TempDir()
	isolateEnv(t, fake, sess, state)
	t.Setenv("CODEX_MODELS", "envmodel")
	t.Setenv("CODEX_TIMEOUT", "3")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"hi"}, &stdout, &stderr)
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "model=envmodel")
	assert.Contains(t, stderr.String(), "timeout=3s")
}

func TestRun_passthroughAndDoubleDash(t *testing.T) {
	// fake codex echoes its argv so we can assert passthrough forwarding.
	fake := writeFakeCodex(t, `#!/bin/sh
echo "args: $*"
exit 0
`)
	sess := t.TempDir()
	state := t.TempDir()
	isolateEnv(t, fake, sess, state)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(),
		[]string{"--models", "m1", "--", "--skip-git-repo-check", "-c", "k=v", "PROMPT"},
		&stdout, &stderr)
	assert.Equal(t, 0, code)
	successPath := strings.TrimSpace(stdout.String())
	content, err := os.ReadFile(successPath)
	require.NoError(t, err)
	body := string(content)
	assert.Contains(t, body, "exec")
	assert.Contains(t, body, "-m m1")
	assert.Contains(t, body, "--skip-git-repo-check")
	assert.Contains(t, body, "-c k=v")
	assert.Contains(t, body, "-- PROMPT")
}

func TestRun_outFileHonoredForFirstAttempt(t *testing.T) {
	fake := writeFakeCodex(t, `#!/bin/sh
echo "content"
exit 0
`)
	sess := t.TempDir()
	state := t.TempDir()
	isolateEnv(t, fake, sess, state)

	outPath := filepath.Join(t.TempDir(), "explicit.out")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(),
		[]string{"--models", "m1", "--out", outPath, "--timeout", "5", "p"},
		&stdout, &stderr)
	assert.Equal(t, 0, code)
	assert.Equal(t, outPath, strings.TrimSpace(stdout.String()))
	content, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "content")
}

func TestRun_capacityFallsThroughViaEnv(t *testing.T) {
	// Different exit behavior per invocation: use a counter file.
	dir := t.TempDir()
	counter := filepath.Join(dir, "n")
	script := `#!/bin/sh
n=0
[ -f "` + counter + `" ] && n=$(cat "` + counter + `")
n=$((n+1))
echo $n > "` + counter + `"
if [ "$n" = "1" ]; then
  echo "Selected model is at capacity"
  exit 1
fi
echo "OK on attempt $n"
exit 0
`
	fake := writeFakeCodex(t, script)
	sess := t.TempDir()
	state := t.TempDir()
	isolateEnv(t, fake, sess, state)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(),
		[]string{"--models", "a,b", "--timeout", "5", "hi"},
		&stdout, &stderr)
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "a: FAIL")
	assert.Contains(t, stderr.String(), "b: OK")
}
