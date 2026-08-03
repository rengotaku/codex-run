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

// writeFakeCodexScript writes an arbitrary POSIX sh script verbatim as a fake
// `codex` binary. Unlike makeFakeCodex (which enumerates fixed modes per
// model), this lets tests control exact line counts and the position of
// fail-pattern text within the output, which the tail-scan tests need.
func writeFakeCodexScript(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake codex uses POSIX sh; not supported on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

// echoedInputWith429Script returns a fake-codex script body (case-branch
// content, no shebang/case wrapper) that mimics an agent echoing input data
// containing "429" early, followed by harmless lines and a trailing success
// marker. Used by R1 and R4 (and their cmd-level analogues C1-C3) to
// reproduce the issue #17 false-positive scenario.
func echoedInputWith429Lines() string {
	var b strings.Builder
	b.WriteString("    echo 'input echo: request id 429 accepted'\n")
	b.WriteString("    echo 'input echo: batch contains 429 total records'\n")
	b.WriteString("    echo 'input echo: row 429 processed'\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "    echo 'processing item %d'\n", i)
	}
	b.WriteString("    echo 'tokens used'\n")
	b.WriteString("    echo '65,699'\n")
	b.WriteString("    echo 'DONE 40'\n")
	b.WriteString("    exit 0\n")
	return b.String()
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

// --- issue #17: fail-pattern tail-scan tests (R1-R5) ---

// R1: early input-echo containing "429" must be ignored when it falls
// outside the tail-scan window; the run should succeed.
func TestRun_tailScan_ignoresEarlyInputEcho429(t *testing.T) {
	script := "#!/bin/sh\n" + echoedInputWith429Lines()
	bin := writeFakeCodexScript(t, script)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:            []string{"only"},
		Prompt:            "hi",
		TimeoutSec:        5,
		CodexBin:          bin,
		FailScanTailLines: 5,
		Stdout:            &stdout,
		Stderr:            &stderr,
	})
	assert.Equal(t, 0, code)
	out := strings.TrimSpace(stdout.String())
	require.NotEmpty(t, out)
	assert.NotContains(t, stderr.String(), "FAIL")
}

// R2: a genuine fail-pattern that falls inside the tail-scan window must
// still be detected (no regression from narrowing the scan range).
func TestRun_tailScan_detectsFailPatternInsideWindow(t *testing.T) {
	script := `#!/bin/sh
model=''
while [ $# -gt 0 ]; do
  case "$1" in
    -m) model="$2"; shift 2 ;;
    *)  shift ;;
  esac
done
case "$model" in
  bad)
    echo 'line1'
    echo 'line2'
    echo 'line3'
    echo 'line4'
    echo 'line5'
    echo 'line6'
    echo 'line7'
    echo 'line8'
    echo 'Selected model is at capacity'
    exit 0
    ;;
  good)
    echo 'OK from good'
    exit 0
    ;;
esac
`
	bin := writeFakeCodexScript(t, script)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:            []string{"bad", "good"},
		Prompt:            "hi",
		TimeoutSec:        5,
		CodexBin:          bin,
		FailScanTailLines: 5,
		Stdout:            &stdout,
		Stderr:            &stderr,
	})
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "bad: FAIL")
	assert.Contains(t, stderr.String(), "fail-pattern in output (exit=0)")
	assert.Contains(t, stderr.String(), "good: OK")
}

// R3: a fail-pattern that falls outside the tail-scan window is
// intentionally missed (documents the known trade-off of scanning only the
// tail; future changes must not silently alter this behavior).
func TestRun_tailScan_missesFailPatternOutsideWindow(t *testing.T) {
	script := `#!/bin/sh
echo 'row 429 processed early'
echo 'harmless 1'
echo 'harmless 2'
echo 'harmless 3'
echo 'harmless 4'
echo 'harmless 5'
echo 'harmless 6'
echo 'harmless 7'
echo 'harmless 8'
echo 'harmless 9'
echo 'harmless 10'
echo 'harmless 11'
exit 0
`
	bin := writeFakeCodexScript(t, script)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:            []string{"only"},
		Prompt:            "hi",
		TimeoutSec:        5,
		CodexBin:          bin,
		FailScanTailLines: 3,
		Stdout:            &stdout,
		Stderr:            &stderr,
	})
	assert.Equal(t, 0, code)
	assert.NotContains(t, stderr.String(), "FAIL")
}

// R4: FailScanTailLines <= 0 is the full-scan escape hatch; it must still
// detect a fail-pattern that a positive tail window would have missed.
func TestRun_tailScan_zeroFallsBackToFullScan(t *testing.T) {
	script := "#!/bin/sh\n" +
		"model=''\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -m) model=\"$2\"; shift 2 ;;\n" +
		"    *)  shift ;;\n" +
		"  esac\n" +
		"done\n" +
		"case \"$model\" in\n" +
		"  bad)\n" + echoedInputWith429Lines() + "    ;;\n" +
		"  good)\n" +
		"    echo 'OK from good'\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n"
	bin := writeFakeCodexScript(t, script)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), Config{
		Models:            []string{"bad", "good"},
		Prompt:            "hi",
		TimeoutSec:        5,
		CodexBin:          bin,
		FailScanTailLines: 0,
		Stdout:            &stdout,
		Stderr:            &stderr,
	})
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "bad: FAIL")
	assert.Contains(t, stderr.String(), "fail-pattern in output (exit=0)")
	assert.Contains(t, stderr.String(), "good: OK")
}

// R5: lastNLines boundary conditions, isolated from the Run() integration
// path so off-by-one / trailing-newline mistakes are caught locally.
func TestLastNLines(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		n    int
		want []byte
	}{
		{
			name: "n<=0 returns input unchanged",
			data: []byte("a\nb\nc\n"),
			n:    0,
			want: []byte("a\nb\nc\n"),
		},
		{
			name: "negative n returns input unchanged",
			data: []byte("a\nb\nc\n"),
			n:    -3,
			want: []byte("a\nb\nc\n"),
		},
		{
			name: "n exceeds total line count returns input unchanged (trailing newline)",
			data: []byte("a\nb\nc\n"),
			n:    10,
			want: []byte("a\nb\nc\n"),
		},
		{
			name: "n exceeds total line count returns input unchanged (no trailing newline)",
			data: []byte("a\nb\nc"),
			n:    10,
			want: []byte("a\nb\nc"),
		},
		{
			name: "5 lines, n=2, trailing newline preserved without duplication",
			data: []byte("l1\nl2\nl3\nl4\nl5\n"),
			n:    2,
			want: []byte("l4\nl5\n"),
		},
		{
			name: "5 lines, n=2, no trailing newline, no line dropped",
			data: []byte("l1\nl2\nl3\nl4\nl5"),
			n:    2,
			want: []byte("l4\nl5"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lastNLines(tc.data, tc.n)
			assert.Equal(t, tc.want, got)
		})
	}
}
