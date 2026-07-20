// Package cli parses codex-run command-line arguments.
//
// codex-run's argument model: any flag not owned by codex-run (--models,
// --out, --timeout, -h/--help, --version) is treated as a positional token
// forwarded verbatim to `codex exec`. The last positional token is always
// PROMPT. `--` can be used to force everything after it to be positional.
//
// This is why the root Cobra command sets DisableFlagParsing: cobra/pflag
// would reject unknown flags, but we intentionally forward them.
package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Args holds the parsed command-line invocation.
type Args struct {
	Models      []string
	OutFile     string
	TimeoutSec  int
	Passthrough []string
	Prompt      string
	ShowHelp    bool
	ShowVersion bool
}

// Defaults returns Args prefilled from the given environment defaults.
// Callers pass CODEX_MODELS / CODEX_TIMEOUT resolved values.
func Defaults(envModels string, envTimeoutSec int) Args {
	return Args{
		Models:     splitModels(envModels),
		TimeoutSec: envTimeoutSec,
	}
}

// Parse consumes argv (excluding program name) and returns Args or an error.
// Errors are user-facing (returned exit code 2 by the caller).
func Parse(argv []string, defaults Args) (Args, error) {
	a := defaults
	positional := make([]string, 0, len(argv))
	sawDoubleDash := false

	i := 0
	for i < len(argv) {
		arg := argv[i]
		if sawDoubleDash {
			positional = append(positional, arg)
			i++
			continue
		}

		switch {
		case arg == "--":
			sawDoubleDash = true
			i++
		case arg == "-h", arg == "--help":
			a.ShowHelp = true
			return a, nil
		case arg == "--version":
			a.ShowVersion = true
			return a, nil
		case arg == "--models":
			if i+1 >= len(argv) {
				return a, errors.New("--models requires a value")
			}
			a.Models = splitModels(argv[i+1])
			i += 2
		case strings.HasPrefix(arg, "--models="):
			a.Models = splitModels(strings.TrimPrefix(arg, "--models="))
			i++
		case arg == "--out":
			if i+1 >= len(argv) {
				return a, errors.New("--out requires a value")
			}
			a.OutFile = argv[i+1]
			i += 2
		case strings.HasPrefix(arg, "--out="):
			a.OutFile = strings.TrimPrefix(arg, "--out=")
			i++
		case arg == "--timeout":
			if i+1 >= len(argv) {
				return a, errors.New("--timeout requires a value")
			}
			n, err := parsePositiveInt(argv[i+1])
			if err != nil {
				return a, fmt.Errorf("--timeout must be a positive integer (got: %s)", argv[i+1])
			}
			a.TimeoutSec = n
			i += 2
		case strings.HasPrefix(arg, "--timeout="):
			raw := strings.TrimPrefix(arg, "--timeout=")
			n, err := parsePositiveInt(raw)
			if err != nil {
				return a, fmt.Errorf("--timeout must be a positive integer (got: %s)", raw)
			}
			a.TimeoutSec = n
			i++
		default:
			positional = append(positional, arg)
			i++
		}
	}

	if len(positional) == 0 {
		return a, errors.New("PROMPT is required")
	}
	a.Prompt = positional[len(positional)-1]
	if a.Prompt == "" {
		return a, errors.New("PROMPT is empty")
	}
	if len(positional) > 1 {
		a.Passthrough = positional[:len(positional)-1]
	}

	if len(a.Models) == 0 {
		return a, errors.New("no models in fallback chain")
	}
	if a.TimeoutSec <= 0 {
		return a, fmt.Errorf("--timeout must be a positive integer (got: %d)", a.TimeoutSec)
	}
	return a, nil
}

func splitModels(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parsePositiveInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("not positive: %d", n)
	}
	return n, nil
}
