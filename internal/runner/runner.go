// Package runner executes the codex-run fallback loop: try each model in
// turn, retry on capacity / rate-limit / timeout, and report the first
// successful attempt's output file path.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// TimeoutExitCode mirrors the GNU `timeout` command's exit code for the
// killed-by-timeout condition. Kept for backward-compatibility with the zsh
// implementation's contract (v0.1.0).
const TimeoutExitCode = 124

// failPatterns matches retryable failures. It fires on either the process's
// non-zero exit or occasionally on exit=0 output when codex prints an error
// yet returns success. Case-insensitive.
var failPatterns = regexp.MustCompile(`(?i)(Selected model is at capacity|Too Many Requests|quota exceeded|rate.?limit|(^|[^0-9])429([^0-9]|$))`)

// UsageReporter is invoked once at the end of a run, regardless of outcome.
// It must never mutate the exit code and should swallow its own errors
// (silent-skip contract).
type UsageReporter func(startTS int64, model string, exitCode int)

// Config drives a single Run invocation.
type Config struct {
	Models      []string
	Passthrough []string
	Prompt      string
	OutFile     string // attempt #1 destination; empty ⇒ mktemp for every attempt
	TimeoutSec  int
	CodexBin    string    // resolved absolute path or PATH-lookupable name
	Stdout      io.Writer // success path is written here on success
	Stderr      io.Writer // progress log
	NowUnix     func() int64
	Reporter    UsageReporter // optional
}

// Run executes the fallback loop and returns the process exit code.
// On success: writes the successful attempt's absolute path to Stdout and
// returns 0.
// On total failure: returns the last attempted model's exit code.
func Run(ctx context.Context, cfg Config) int {
	if cfg.NowUnix == nil {
		cfg.NowUnix = func() int64 { return time.Now().Unix() }
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	if _, err := exec.LookPath(cfg.CodexBin); err != nil {
		fmt.Fprintf(cfg.Stderr, "codex-run: error: '%s' not found in PATH (set CODEX_BIN to override)\n", cfg.CodexBin)
		return 127
	}

	startTS := cfg.NowUnix()
	var attemptLog []string
	lastExit := 0
	lastPath := ""
	successModel := ""
	success := false

	for i, model := range cfg.Models {
		safeModel := sanitizeModel(model)
		outPath, err := chooseOutPath(cfg.OutFile, safeModel, i == 0)
		if err != nil {
			fmt.Fprintf(cfg.Stderr, "codex-run: %v\n", err)
			return 2
		}

		fmt.Fprintf(cfg.Stderr, "[attempt %d/%d] model=%s out=%s timeout=%ds\n",
			i+1, len(cfg.Models), model, outPath, cfg.TimeoutSec)

		ec := runAttempt(ctx, cfg, model, outPath)
		lastExit = ec
		lastPath = outPath

		reason := ""
		switch {
		case ec == 0:
			if fileMatchesFailPattern(outPath) {
				reason = "fail-pattern in output (exit=0)"
			} else {
				success = true
				successModel = model
				attemptLog = append(attemptLog, fmt.Sprintf("  [%d] %s: OK -> %s", i+1, model, outPath))
				fmt.Fprintln(cfg.Stderr, "  -> OK")
			}
		case ec == TimeoutExitCode:
			reason = fmt.Sprintf("timeout after %ds", cfg.TimeoutSec)
		case fileMatchesFailPattern(outPath):
			reason = fmt.Sprintf("fail-pattern (exit=%d)", ec)
		default:
			reason = fmt.Sprintf("exit=%d", ec)
		}
		if success {
			break
		}
		attemptLog = append(attemptLog, fmt.Sprintf("  [%d] %s: FAIL (%s) trace=%s", i+1, model, reason, outPath))
		fmt.Fprintf(cfg.Stderr, "  -> FAIL: %s\n", reason)
	}

	fmt.Fprintln(cfg.Stderr)
	fmt.Fprintln(cfg.Stderr, "attempts:")
	for _, line := range attemptLog {
		fmt.Fprintln(cfg.Stderr, line)
	}

	reportModel := successModel
	if reportModel == "" && len(cfg.Models) > 0 {
		reportModel = cfg.Models[len(cfg.Models)-1]
	}
	if cfg.Reporter != nil {
		cfg.Reporter(startTS, reportModel, lastExit)
	}

	if success {
		fmt.Fprintln(cfg.Stdout, lastPath)
		return 0
	}
	fmt.Fprintln(cfg.Stderr)
	fmt.Fprintf(cfg.Stderr, "codex-run: all %d model(s) failed\n", len(cfg.Models))
	return lastExit
}

// runAttempt invokes codex once with a per-attempt timeout and captures its
// combined stdout+stderr into outPath. Returns the child's exit code, or
// TimeoutExitCode when the deadline killed the child.
func runAttempt(ctx context.Context, cfg Config, model, outPath string) int {
	f, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "codex-run: cannot open out %s: %v\n", outPath, err)
		return 2
	}
	defer f.Close()

	attemptCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	args := []string{"exec", "-m", model}
	args = append(args, cfg.Passthrough...)
	args = append(args, "--", cfg.Prompt)
	cmd := exec.CommandContext(attemptCtx, cfg.CodexBin, args...)
	cmd.Stdout = f
	cmd.Stderr = f

	err = cmd.Run()
	if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) {
		return TimeoutExitCode
	}
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(cfg.Stderr, "codex-run: exec error: %v\n", err)
	return 1
}

// chooseOutPath decides where to write the given attempt's output. Attempt #1
// honors --out FILE; every other attempt (and #1 when --out is unset) uses a
// unique mktemp-style path to survive concurrent invocations.
func chooseOutPath(outFile, safeModel string, isFirstAttempt bool) (string, error) {
	if outFile != "" && isFirstAttempt {
		f, err := os.Create(outFile)
		if err != nil {
			return "", fmt.Errorf("cannot write to --out '%s': %w", outFile, err)
		}
		_ = f.Close()
		return absPath(outFile), nil
	}
	pattern := fmt.Sprintf("codex-run-%s-*", safeModel)
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("mktemp failed: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	return name, nil
}

func absPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// sanitizeModel replaces characters that would be awkward inside a temp
// filename with underscores. Mirrors the zsh implementation's
// ${MODEL//[^A-Za-z0-9._-]/_} substitution.
func sanitizeModel(m string) string {
	var b strings.Builder
	b.Grow(len(m))
	for _, r := range m {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func fileMatchesFailPattern(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return failPatterns.Match(data)
}
