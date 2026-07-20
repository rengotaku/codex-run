package usage

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 21, 3, 4, 5, 0, time.FixedZone("JST", 9*3600))
}

func writeSession(t *testing.T, dir, name string, mtime time.Time) string {
	t.Helper()
	sub := filepath.Join(dir, "2026", "07")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	src := filepath.Join("testdata", "rollout-two-events.jsonl")
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	path := filepath.Join(sub, name)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
	return path
}

func TestReport_writesSummaryAndTSV(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "sessions")
	require.NoError(t, os.MkdirAll(sess, 0o755))
	start := time.Now()
	writeSession(t, sess, "rollout-1.jsonl", start)

	logPath := filepath.Join(dir, "state", "codex-run", "usage.log")
	var buf bytes.Buffer
	Report(Input{
		StartTS:     start.Unix(),
		Model:       "gpt-5.6-sol",
		ExitCode:    0,
		SessionsDir: sess,
		UsageLog:    logPath,
		Stderr:      &buf,
		Now:         fixedNow(),
	})
	assert.Contains(t, buf.String(), "usage: plan=plus used=3.5%/7d tokens=in1500/out350",
		"summary should reflect last event only. got: %q", buf.String())

	logData, err := os.ReadFile(logPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(logData), "\n"), "\n")
	require.Len(t, lines, 2, "header + 1 row")
	assert.Equal(t, "ts\tplan\tused_pct\twindow_min\ttokens_in\ttokens_out\tmodel\texit", lines[0])
	assert.Equal(t, "2026-07-21T03:04:05+0900\tplus\t3.5\t10080\t1500\t350\tgpt-5.6-sol\t0", lines[1])
}

func TestReport_appendsWithoutDuplicateHeader(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "sessions")
	require.NoError(t, os.MkdirAll(sess, 0o755))
	start := time.Now()
	writeSession(t, sess, "rollout-1.jsonl", start)
	logPath := filepath.Join(dir, "usage.log")

	for i := 0; i < 3; i++ {
		Report(Input{
			StartTS:     start.Unix(),
			Model:       "m",
			ExitCode:    0,
			SessionsDir: sess,
			UsageLog:    logPath,
			Stderr:      io.Discard,
			Now:         fixedNow(),
		})
	}
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	require.Len(t, lines, 4, "1 header + 3 rows")
	assert.True(t, strings.HasPrefix(lines[0], "ts\t"))
	for i := 1; i < 4; i++ {
		assert.False(t, strings.HasPrefix(lines[i], "ts\t"), "row %d must not repeat header", i)
	}
}

func TestReport_missingSessionsDir_silentSkip(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "usage.log")
	var buf bytes.Buffer
	Report(Input{
		StartTS:     time.Now().Unix(),
		Model:       "m",
		ExitCode:    0,
		SessionsDir: "/nonexistent/codex/sessions",
		UsageLog:    logPath,
		Stderr:      &buf,
		Now:         fixedNow(),
	})
	assert.Contains(t, buf.String(), "sessions dir not found")
	_, err := os.Stat(logPath)
	assert.True(t, os.IsNotExist(err), "usage.log must not be created on skip")
}

func TestReport_noFreshRollout_silentSkip(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "sessions")
	require.NoError(t, os.MkdirAll(sess, 0o755))
	old := time.Now().Add(-24 * time.Hour)
	writeSession(t, sess, "rollout-old.jsonl", old)
	logPath := filepath.Join(dir, "usage.log")

	var buf bytes.Buffer
	Report(Input{
		StartTS:     time.Now().Unix(),
		Model:       "m",
		ExitCode:    0,
		SessionsDir: sess,
		UsageLog:    logPath,
		Stderr:      &buf,
		Now:         fixedNow(),
	})
	assert.Contains(t, buf.String(), "no-fresh-rollout")
	_, err := os.Stat(logPath)
	assert.True(t, os.IsNotExist(err))
}

func TestReport_selectsNewestMatchingFile(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "sessions")
	require.NoError(t, os.MkdirAll(sess, 0o755))
	start := time.Now()
	older := writeSession(t, sess, "rollout-older.jsonl", start)
	newer := writeSession(t, sess, "rollout-newer.jsonl", start.Add(2*time.Second))
	_ = older
	_ = newer
	logPath := filepath.Join(dir, "usage.log")

	var buf bytes.Buffer
	Report(Input{
		StartTS:     start.Unix(),
		Model:       "m",
		ExitCode:    0,
		SessionsDir: sess,
		UsageLog:    logPath,
		Stderr:      &buf,
		Now:         fixedNow(),
	})
	assert.Contains(t, buf.String(), "usage: plan=plus")
}

func TestWindowLabel(t *testing.T) {
	assert.Equal(t, "-", windowLabel("-"))
	assert.Equal(t, "10m", windowLabel("10"))
	assert.Equal(t, "7d", windowLabel("10080"))
	assert.Equal(t, "60m", windowLabel("60"))
	assert.Equal(t, "1d", windowLabel("1440"))
}

func TestFormatFloat(t *testing.T) {
	assert.Equal(t, "1500", formatFloat(1500.0))
	assert.Equal(t, "3.5", formatFloat(3.5))
	assert.Equal(t, "0", formatFloat(0))
}
