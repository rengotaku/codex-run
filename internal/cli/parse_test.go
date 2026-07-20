package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_defaults(t *testing.T) {
	def := Defaults("gpt-5.6-sol,gpt-5.5", 600)
	a, err := Parse([]string{"hello"}, def)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-5.6-sol", "gpt-5.5"}, a.Models)
	assert.Equal(t, 600, a.TimeoutSec)
	assert.Empty(t, a.Passthrough)
	assert.Equal(t, "hello", a.Prompt)
}

func TestParse_flagsAndPassthrough(t *testing.T) {
	def := Defaults("m1,m2", 600)
	a, err := Parse([]string{
		"--models", "a,b,c",
		"--timeout", "30",
		"--out", "/tmp/out.txt",
		"--skip-git-repo-check",
		"-c", "key=val",
		"PROMPT",
	}, def)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, a.Models)
	assert.Equal(t, 30, a.TimeoutSec)
	assert.Equal(t, "/tmp/out.txt", a.OutFile)
	assert.Equal(t, []string{"--skip-git-repo-check", "-c", "key=val"}, a.Passthrough)
	assert.Equal(t, "PROMPT", a.Prompt)
}

func TestParse_equalsForm(t *testing.T) {
	def := Defaults("x", 1)
	a, err := Parse([]string{"--models=one,two", "--timeout=45", "--out=/x", "p"}, def)
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, a.Models)
	assert.Equal(t, 45, a.TimeoutSec)
	assert.Equal(t, "/x", a.OutFile)
	assert.Equal(t, "p", a.Prompt)
}

func TestParse_doubleDash(t *testing.T) {
	def := Defaults("m", 1)
	a, err := Parse([]string{"--", "--models", "not-a-flag", "prompt"}, def)
	require.NoError(t, err)
	assert.Equal(t, []string{"--models", "not-a-flag"}, a.Passthrough)
	assert.Equal(t, "prompt", a.Prompt)
	assert.Equal(t, []string{"m"}, a.Models)
}

func TestParse_help(t *testing.T) {
	def := Defaults("m", 1)
	a, err := Parse([]string{"-h"}, def)
	require.NoError(t, err)
	assert.True(t, a.ShowHelp)

	a, err = Parse([]string{"--help"}, def)
	require.NoError(t, err)
	assert.True(t, a.ShowHelp)
}

func TestParse_version(t *testing.T) {
	def := Defaults("m", 1)
	a, err := Parse([]string{"--version"}, def)
	require.NoError(t, err)
	assert.True(t, a.ShowVersion)
}

func TestParse_errors(t *testing.T) {
	def := Defaults("m", 600)

	_, err := Parse([]string{}, def)
	assert.ErrorContains(t, err, "PROMPT is required")

	_, err = Parse([]string{""}, def)
	assert.ErrorContains(t, err, "PROMPT is empty")

	_, err = Parse([]string{"--models"}, def)
	assert.ErrorContains(t, err, "--models requires")

	_, err = Parse([]string{"--out"}, def)
	assert.ErrorContains(t, err, "--out requires")

	_, err = Parse([]string{"--timeout"}, def)
	assert.ErrorContains(t, err, "--timeout requires")

	_, err = Parse([]string{"--timeout", "abc", "p"}, def)
	assert.ErrorContains(t, err, "positive integer")

	_, err = Parse([]string{"--timeout", "0", "p"}, def)
	assert.ErrorContains(t, err, "positive integer")

	_, err = Parse([]string{"--models=,,,", "p"}, Defaults("", 600))
	assert.ErrorContains(t, err, "no models")
}
