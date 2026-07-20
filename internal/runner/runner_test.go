package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFakeCodex writes a POSIX sh script that behaves like `codex exec` based
// on the given `spec` map: modelName -> {"OK"|"CAPACITY"|"TIMEOUT"|"FAIL"|"429"}.
// The script inspects the -m flag to pick a mode.
func makeFakeCodex(t *testing.T, spec map[string]string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake codex uses POSIX sh; not supported on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# fake codex generated for testing\n")
	b.WriteString("model=''\n")
	b.WriteString("while [ $# -gt 0 ]; do\n")
	b.WriteString("  case \"$1\" in\n")
	b.WriteString("    -m) model=\"$2\"; shift 2 ;;\n")
	b.WriteString("    *)  shift ;;\n")
	b.WriteString("  esac\n")
	b.WriteString("done\n")
	b.WriteString("case \"$model\" in\n")
	for name, mode := range spec {
		fmt.Fprintf(&b, "  %s)\n", shQuote(name))
		switch mode {
		case "OK":
			fmt.Fprintf(&b, "    echo 'OK from %s'; exit 0 ;;\n", name)
		case "CAPACITY":
			b.WriteString("    echo 'Selected model is at capacity'; exit 1 ;;\n")
		case "TIMEOUT":
			b.WriteString("    sleep 30; exit 0 ;;\n")
		case "FAIL":
			b.WriteString("    echo 'boom'; exit 3 ;;\n")
		case "429":
			b.WriteString("    echo 'HTTP 429 Too Many Requests'; exit 1 ;;\n")
		case "OK_BUT_CAPACITY":
			b.WriteString("    echo 'Selected model is at capacity'; exit 0 ;;\n")
		default:
			t.Fatalf("unknown fake codex mode %q", mode)
		}
	}
	b.WriteString("  *) echo \"unknown model: $model\"; exit 99 ;;\n")
	b.WriteString("esac\n")

	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o755))
	return path
}

// shQuote escapes a case pattern for POSIX sh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func TestRun_successOnFirstAttempt(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{"good": "OK"})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:     []string{"good"},
		Prompt:     "hi",
		TimeoutSec: 5,
		CodexBin:   bin,
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	assert.Equal(t, 0, code)
	out := strings.TrimSpace(stdout.String())
	require.NotEmpty(t, out)
	assert.True(t, filepath.IsAbs(out), "success path must be absolute: %s", out)
	content, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(content), "OK from good")
	assert.Contains(t, stderr.String(), "[attempt 1/1]")
	assert.Contains(t, stderr.String(), "-> OK")
}

func TestRun_capacityFallsThrough(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{
		"capa": "CAPACITY",
		"good": "OK",
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:     []string{"capa", "good"},
		Prompt:     "hi",
		TimeoutSec: 5,
		CodexBin:   bin,
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "[attempt 1/2] model=capa")
	assert.Contains(t, stderr.String(), "[attempt 2/2] model=good")
	assert.Contains(t, stderr.String(), "capa: FAIL")
	assert.Contains(t, stderr.String(), "good: OK")
}

func TestRun_httpFourTwentyNine(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{
		"rl":   "429",
		"good": "OK",
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:     []string{"rl", "good"},
		Prompt:     "hi",
		TimeoutSec: 5,
		CodexBin:   bin,
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "rl: FAIL")
}

func TestRun_capacityWithZeroExit(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{
		"weird": "OK_BUT_CAPACITY",
		"good":  "OK",
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:     []string{"weird", "good"},
		Prompt:     "hi",
		TimeoutSec: 5,
		CodexBin:   bin,
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "fail-pattern in output (exit=0)")
}

func TestRun_allFail_returnsLastExit(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{
		"a": "FAIL",
		"b": "FAIL",
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:     []string{"a", "b"},
		Prompt:     "hi",
		TimeoutSec: 5,
		CodexBin:   bin,
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	assert.Equal(t, 3, code)
	assert.Empty(t, strings.TrimSpace(stdout.String()))
	assert.Contains(t, stderr.String(), "all 2 model(s) failed")
}

func TestRun_timeoutTriggersFallback(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{
		"slow": "TIMEOUT",
		"good": "OK",
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:     []string{"slow", "good"},
		Prompt:     "hi",
		TimeoutSec: 1,
		CodexBin:   bin,
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "slow: FAIL (timeout after 1s)")
}

func TestRun_outFileHonored_forFirstAttempt(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{"good": "OK"})
	tmp := filepath.Join(t.TempDir(), "explicit.txt")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:     []string{"good"},
		Prompt:     "hi",
		OutFile:    tmp,
		TimeoutSec: 5,
		CodexBin:   bin,
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	assert.Equal(t, 0, code)
	assert.Equal(t, tmp, strings.TrimSpace(stdout.String()))
	content, err := os.ReadFile(tmp)
	require.NoError(t, err)
	assert.Contains(t, string(content), "OK from good")
}

func TestRun_missingCodexBinary(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:     []string{"m"},
		Prompt:     "hi",
		TimeoutSec: 1,
		CodexBin:   "/definitely/not/a/binary/here",
		Stderr:     &stderr,
	})
	assert.Equal(t, 127, code)
	assert.Contains(t, stderr.String(), "not found in PATH")
}

func TestRun_reporterInvokedOnce(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{"good": "OK"})
	var calls int
	var gotModel string
	var gotExit int
	code := Run(context.Background(), Config{
		Models:     []string{"good"},
		Prompt:     "hi",
		TimeoutSec: 5,
		CodexBin:   bin,
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		Reporter: func(_ int64, m string, e int) {
			calls++
			gotModel = m
			gotExit = e
		},
	})
	assert.Equal(t, 0, code)
	assert.Equal(t, 1, calls)
	assert.Equal(t, "good", gotModel)
	assert.Equal(t, 0, gotExit)
}

func TestRun_reporterOnTotalFailureUsesLastModel(t *testing.T) {
	bin := makeFakeCodex(t, map[string]string{
		"a": "FAIL",
		"b": "FAIL",
	})
	var gotModel string
	var gotExit int
	_ = Run(context.Background(), Config{
		Models:     []string{"a", "b"},
		Prompt:     "hi",
		TimeoutSec: 5,
		CodexBin:   bin,
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		Reporter: func(_ int64, m string, e int) {
			gotModel = m
			gotExit = e
		},
	})
	assert.Equal(t, "b", gotModel)
	assert.Equal(t, 3, gotExit)
}

func TestSanitizeModel(t *testing.T) {
	assert.Equal(t, "gpt-5.6-sol", sanitizeModel("gpt-5.6-sol"))
	assert.Equal(t, "abc_def", sanitizeModel("abc/def"))
	assert.Equal(t, "a_b_c", sanitizeModel("a b c"))
	assert.Equal(t, "___", sanitizeModel("///"))
}
