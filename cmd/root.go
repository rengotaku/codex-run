// Package cmd wires the codex-run CLI. It intentionally uses
// DisableFlagParsing: any flag not owned by codex-run (--models, --out,
// --timeout, -h/--help, --version) is forwarded verbatim to `codex exec`.
// The last positional argument is always PROMPT.
//
// This mirrors the v0.1.0 zsh implementation's CLI contract, so callers that
// relied on `codex-run --skip-git-repo-check "prompt"` keep working.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/rengotaku/codex-run/internal/cli"
	"github.com/rengotaku/codex-run/internal/runner"
	"github.com/rengotaku/codex-run/internal/usage"
)

// Version is set at build time via -ldflags "-X ...cmd.Version=..." for
// released binaries; tests and `go run` see "dev".
var Version = "dev"

// DefaultModels is the fallback chain used when neither --models nor
// CODEX_MODELS is set.
const DefaultModels = "gpt-5.6-sol,gpt-5.6-luna,gpt-5.6-terra,gpt-5.5"

// DefaultTimeoutSec is the per-model wall-clock cap.
const DefaultTimeoutSec = 600

const usageText = `codex-run — fallback wrapper around ` + "`codex exec`" + `

Usage:
  codex-run [OPTIONS] [codex-exec-args...] PROMPT

Options:
  --models a,b,c    Comma-separated model fallback chain
                    (env CODEX_MODELS; default: ` + DefaultModels + `)
  --out FILE        Write attempt #1 to FILE (retries always use mktemp)
  --timeout SEC     Per-model timeout in seconds
                    (env CODEX_TIMEOUT; default: 600)
  -h, --help        Show this help
  --version         Show version

Any flag not listed above is forwarded verbatim to ` + "`codex exec`" + `
(e.g. --skip-git-repo-check, -c key=val). PROMPT is always the last
positional argument. Use ` + "`--`" + ` to force everything after it to be
treated as ` + "`codex exec`" + ` args + trailing PROMPT.

Output contract:
  stdout : on success, absolute path of the successful attempt's output file (one line)
  stderr : progress, per-attempt result, final summary
  exit   : 0 on success; on total failure, the last attempted model's exit code

Trace files (per attempt) are never deleted so failures remain diagnosable.
`

// rootCmd is intentionally minimal — the heavy lifting is in run() below so
// tests can drive it via Args/Stdout/Stderr injection.
var rootCmd = &cobra.Command{
	Use:                "codex-run",
	Short:              "Fallback wrapper around `codex exec`",
	DisableFlagParsing: true, // see package doc; we forward unknown flags to codex exec
	SilenceUsage:       true,
	SilenceErrors:      true,
	RunE: func(cmd *cobra.Command, args []string) error {
		exit := run(cmd.Context(), args, cmd.OutOrStdout(), cmd.ErrOrStderr())
		if exit == 0 {
			return nil
		}
		return &exitError{code: exit}
	},
}

// Execute runs the root command with a background context and returns the
// process exit code to main.
func Execute() int {
	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		return 0
	}
	var xerr *exitError
	if errors.As(err, &xerr) {
		return xerr.code
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func run(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	defaults := cli.Defaults(envOr("CODEX_MODELS", DefaultModels), envIntOr("CODEX_TIMEOUT", DefaultTimeoutSec))
	parsed, err := cli.Parse(argv, defaults)
	if err != nil {
		fmt.Fprintf(stderr, "codex-run: %s\n\n%s", err, usageText)
		return 2
	}
	if parsed.ShowHelp {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if parsed.ShowVersion {
		fmt.Fprintf(stdout, "codex-run %s\n", Version)
		return 0
	}

	home := mustHome()
	codexBin := envOr("CODEX_BIN", "codex")
	sessionsDir := envOr("CODEX_SESSIONS_DIR", filepath.Join(home, ".codex", "sessions"))
	stateDir := filepath.Join(envOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "codex-run")
	usageLog := filepath.Join(stateDir, "usage.log")

	reporter := func(startTS int64, model string, exitCode int) {
		usage.Report(usage.Input{
			StartTS:     startTS,
			Model:       model,
			ExitCode:    exitCode,
			SessionsDir: sessionsDir,
			UsageLog:    usageLog,
			Stderr:      stderr,
		})
	}

	return runner.Run(ctx, runner.Config{
		Models:      parsed.Models,
		Passthrough: parsed.Passthrough,
		Prompt:      parsed.Prompt,
		OutFile:     parsed.OutFile,
		TimeoutSec:  parsed.TimeoutSec,
		CodexBin:    codexBin,
		Stdout:      stdout,
		Stderr:      stderr,
		Reporter:    reporter,
	})
}

type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func mustHome() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "/tmp"
}
