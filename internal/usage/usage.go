// Package usage extracts rate_limits / token_count fields from the most
// recent codex rollout jsonl file and appends a TSV row to usage.log.
//
// Contract: every failure path is silent — at most one line to stderr, and
// the process exit code is never affected. Any panic here would violate the
// runner's post-run guarantee.
package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Input drives one Report call.
type Input struct {
	StartTS     int64
	Model       string
	ExitCode    int
	SessionsDir string    // usually ~/.codex/sessions
	UsageLog    string    // full path; parent dir is created if missing
	Stderr      io.Writer // 1-line summary + optional warnings
	Now         time.Time // stamped into the TSV row; zero ⇒ time.Now()
}

// Report reads the newest rollout jsonl modified at/after StartTS-1, extracts
// the last token_count / rate_limits payloads, and appends one TSV row. On
// any failure it writes a single warning line to Stderr and returns.
func Report(in Input) {
	if in.Stderr == nil {
		in.Stderr = io.Discard
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	if in.SessionsDir == "" {
		fmt.Fprintln(in.Stderr, "usage: sessions dir not set; skip")
		return
	}
	if _, err := os.Stat(in.SessionsDir); err != nil {
		fmt.Fprintf(in.Stderr, "usage: sessions dir not found (%s); skip\n", in.SessionsDir)
		return
	}
	if in.UsageLog == "" {
		fmt.Fprintln(in.Stderr, "usage: log path not set; skip")
		return
	}
	if err := os.MkdirAll(filepath.Dir(in.UsageLog), 0o755); err != nil {
		fmt.Fprintf(in.Stderr, "usage: cannot create %s; skip\n", filepath.Dir(in.UsageLog))
		return
	}

	path, err := newestRolloutAfter(in.SessionsDir, in.StartTS-1)
	if err != nil {
		fmt.Fprintf(in.Stderr, "usage: walk error: %v; skip\n", err)
		return
	}
	if path == "" {
		fmt.Fprintln(in.Stderr, "usage: skip:no-fresh-rollout")
		return
	}

	summary, err := parseRollout(path)
	if err != nil {
		fmt.Fprintf(in.Stderr, "usage: skip:read-error:%v\n", err)
		return
	}

	fmt.Fprintln(in.Stderr, formatSummary(summary))
	if err := appendTSVRow(in.UsageLog, summary, in.Model, in.ExitCode, in.Now); err != nil {
		fmt.Fprintf(in.Stderr, "usage: cannot append to %s: %v\n", in.UsageLog, err)
	}
}

// Summary holds the extracted, formatted-for-display usage numbers. Any
// missing field is represented as "-".
type Summary struct {
	Plan      string
	UsedPct   string
	Window    string
	TokensIn  string
	TokensOut string
}

func newestRolloutAfter(root string, minMTime int64) (string, error) {
	var newestPath string
	var newestMT int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		mt := info.ModTime().Unix()
		if mt < minMTime {
			return nil
		}
		if mt > newestMT {
			newestMT = mt
			newestPath = p
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return newestPath, nil
}

func parseRollout(path string) (Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return Summary{}, err
	}
	defer f.Close()

	summary := Summary{Plan: "-", UsedPct: "-", Window: "-", TokensIn: "-", TokensOut: "-"}
	var lastIn, lastOut *float64
	var lastPlan string
	var lastPct *float64
	var lastWindow *float64
	seenTC := false
	seenRL := false

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var obj struct {
			Payload struct {
				Type string `json:"type"`
				Info struct {
					TotalTokenUsage struct {
						InputTokens  *float64 `json:"input_tokens"`
						OutputTokens *float64 `json:"output_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
				RateLimits *struct {
					PlanType string `json:"plan_type"`
					Primary  *struct {
						UsedPercent   *float64 `json:"used_percent"`
						WindowMinutes *float64 `json:"window_minutes"`
					} `json:"primary"`
				} `json:"rate_limits"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		if obj.Payload.Type != "token_count" {
			continue
		}
		tot := obj.Payload.Info.TotalTokenUsage
		if tot.InputTokens != nil || tot.OutputTokens != nil {
			lastIn = tot.InputTokens
			lastOut = tot.OutputTokens
			seenTC = true
		}
		if rl := obj.Payload.RateLimits; rl != nil {
			lastPlan = rl.PlanType
			if rl.Primary != nil {
				lastPct = rl.Primary.UsedPercent
				lastWindow = rl.Primary.WindowMinutes
			}
			seenRL = true
		}
	}
	if err := sc.Err(); err != nil {
		return Summary{}, err
	}

	if seenTC {
		if lastIn != nil {
			summary.TokensIn = formatFloat(*lastIn)
		}
		if lastOut != nil {
			summary.TokensOut = formatFloat(*lastOut)
		}
	}
	if seenRL {
		if lastPlan != "" {
			summary.Plan = lastPlan
		}
		if lastPct != nil {
			summary.UsedPct = formatFloat(*lastPct)
		}
		if lastWindow != nil {
			summary.Window = formatFloat(*lastWindow)
		}
	}
	return summary, nil
}

func formatSummary(s Summary) string {
	if s.UsedPct != "-" && s.Window != "-" {
		win := windowLabel(s.Window)
		return fmt.Sprintf("usage: plan=%s used=%s%%/%s tokens=in%s/out%s",
			s.Plan, s.UsedPct, win, s.TokensIn, s.TokensOut)
	}
	return fmt.Sprintf("usage: plan=%s tokens=in%s/out%s", s.Plan, s.TokensIn, s.TokensOut)
}

// windowLabel renders window_minutes as `Nd` when >= 1 day, else `Nm`.
func windowLabel(window string) string {
	if window == "-" {
		return "-"
	}
	if n, err := strconv.Atoi(window); err == nil && n >= 1440 {
		return fmt.Sprintf("%dd", n/1440)
	}
	return window + "m"
}

func appendTSVRow(path string, s Summary, model string, exitCode int, now time.Time) error {
	ts := now.Format("2006-01-02T15:04:05-0700")
	row := strings.Join([]string{
		ts, s.Plan, s.UsedPct, s.Window, s.TokensIn, s.TokensOut, defaultDash(model), strconv.Itoa(exitCode),
	}, "\t")

	needHeader := true
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		needHeader = false
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if needHeader {
		if _, werr := f.WriteString("ts\tplan\tused_pct\twindow_min\ttokens_in\ttokens_out\tmodel\texit\n"); werr != nil {
			return werr
		}
	}
	_, err = f.WriteString(row + "\n")
	return err
}

func defaultDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatFloat renders whole-number floats without the trailing `.0` (matches
// the Python "%g" formatter used in the v0.1.0 zsh implementation).
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
