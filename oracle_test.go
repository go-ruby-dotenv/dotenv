// Copyright (c) the go-ruby-dotenv/dotenv authors
//
// SPDX-License-Identifier: BSD-3-Clause

package dotenv

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// oracleRuby locates a usable `ruby` whose RUBY_VERSION >= "4.0" and that has the
// `dotenv` gem installed, once. The differential oracle skips itself when any of
// those is missing (the qemu cross-arch lanes, Windows, and older rubies), so the
// deterministic golden suite alone drives the 100% gate there. Gating on >= 4.0
// pins the oracle to the MRI generation this port tracks.
func oracleRuby(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping MRI oracle")
	}
	// Require RUBY_VERSION >= "4.0".
	out, err := exec.Command(bin, "-e", `print(RUBY_VERSION >= "4.0" ? "y" : "n")`).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "y" {
		t.Skipf("ruby < 4.0 (or probe failed: %v); skipping MRI oracle", err)
	}
	// Require the dotenv gem.
	if err := exec.Command(bin, "-e", "require 'dotenv/parser'").Run(); err != nil {
		t.Skip("dotenv gem not installed; skipping MRI oracle")
	}
	return bin
}

// rubyParse runs `Dotenv::Parser.call(src)` in MRI under a fixed ambient ENV and
// returns the resulting Hash as JSON (or {"__error__": msg} on a FormatError). It
// $stdout.binmode's so no text-mode translation pollutes the bytes.
func rubyParse(t *testing.T, bin, src string, overwrite bool) string {
	t.Helper()
	script := `
$stdout.binmode
require "dotenv/parser"
require "json"
ENV.delete("DOTENV_LINEBREAK_MODE")
ENV["ENVHOST"] = "env.example.com"
src = $stdin.read
begin
  print JSON.generate(Dotenv::Parser.call(src, overwrite: ` + boolLit(overwrite) + `))
rescue Dotenv::FormatError => e
  print JSON.generate({"__error__" => e.message})
end
`
	cmd := exec.Command(bin, "-e", script)
	cmd.Stdin = strings.NewReader(src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby error: %v\nsrc=%q\noutput:\n%s", err, src, out)
	}
	return string(out)
}

func boolLit(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestOracleDifferential re-derives the golden corpus live against MRI's
// `Dotenv::Parser.call` and asserts this package's Parse agrees byte-for-byte. It
// covers every value form (unquoted / single- / double-quoted, empty), inline and
// full-line comments, `export`, YAML-style `KEY: value`, variable interpolation
// (`$VAR` / `${VAR}`, brace edge cases, ENV fallback, escaping), the newline /
// linebreak-mode expansion, command-substitution parsing (escaped and unbalanced),
// carriage returns, multiline quoted values, and the FormatError path.
func TestOracleDifferential(t *testing.T) {
	bin := oracleRuby(t)

	var golden map[string]json.RawMessage
	if err := json.Unmarshal(goldenJSON, &golden); err != nil {
		t.Fatalf("decode golden.json: %v", err)
	}
	for src := range golden {
		t.Run(shortName(src), func(t *testing.T) {
			want := rubyParse(t, bin, src, false)
			assertParseMatchesJSON(t, src, want)
		})
	}
}

// TestOracleOverwrite exercises the overwrite path against MRI: with a key present
// in ENV, overwrite:false keeps the ENV value while overwrite:true takes the file
// value.
func TestOracleOverwrite(t *testing.T) {
	bin := oracleRuby(t)
	// ENVHOST is set in the oracle ENV; use it as the "already present" key.
	src := "ENVHOST=fromfile"
	for _, overwrite := range []bool{false, true} {
		want := rubyParse(t, bin, src, overwrite)
		lookup := func(k string) (string, bool) {
			if k == "ENVHOST" {
				return "env.example.com", true
			}
			return "", false
		}
		got, err := Parse(src, overwrite, lookup)
		if err != nil {
			t.Fatalf("Parse overwrite=%v: %v", overwrite, err)
		}
		assertJSONEqualsMap(t, src, want, got)
	}
}

// TestOracleCommandExecution confirms the command-substitution seam is faithful:
// the Script this package records is exactly what MRI would (and does) run, so
// executing it reproduces the gem's inlined result. It runs each recorded script
// through /bin/sh and checks the chomped output matches MRI's full parse.
func TestOracleCommandExecution(t *testing.T) {
	bin := oracleRuby(t)
	cases := []string{
		"echo=$(echo hello)",
		"VAR1=var1\ninterp=$(echo \"VAR1 is $VAR1\")",
		"FOO=$(echo bar)\nBAR=$(echo $FOO)",
	}
	for _, src := range cases {
		t.Run(shortName(src), func(t *testing.T) {
			// MRI's real result (it shells out).
			wantJSON := rubyParse(t, bin, src, false)
			var want map[string]string
			if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
				t.Fatalf("decode MRI result: %v (%s)", err, wantJSON)
			}
			// This package: parse with the command-execution seam wired to /bin/sh,
			// which executes each command inline (so chains resolve) exactly as MRI
			// shells out.
			run := func(script string) string {
				out, err := exec.Command("/bin/sh", "-c", script).Output()
				if err != nil {
					t.Fatalf("exec %q: %v", script, err)
				}
				return string(out)
			}
			hash, _, err := ParseWithRunner(src, false, oracleEnvLookup, run)
			if err != nil {
				t.Fatal(err)
			}
			for k, wv := range want {
				if gv, _ := hash.Get(k); gv != wv {
					t.Fatalf("src=%q key=%q: seam+exec = %q, MRI = %q", src, k, gv, wv)
				}
			}
		})
	}
}

// assertParseMatchesJSON parses src here and asserts it equals the JSON MRI
// produced (a map, or {"__error__": msg}).
func assertParseMatchesJSON(t *testing.T, src, wantJSON string) {
	t.Helper()
	var em map[string]string
	if err := json.Unmarshal([]byte(wantJSON), &em); err != nil {
		t.Fatalf("decode MRI JSON %q: %v", wantJSON, err)
	}
	got, err := Parse(src, false, oracleEnvLookup)
	if msg, isErr := em["__error__"]; isErr && len(em) == 1 {
		if err == nil {
			t.Fatalf("Parse(%q) = no error, MRI raised %q", src, msg)
		}
		if err.Error() != msg {
			t.Fatalf("Parse(%q) error = %q, MRI = %q", src, err.Error(), msg)
		}
		return
	}
	if err != nil {
		t.Fatalf("Parse(%q) errored (%v) but MRI did not", src, err)
	}
	assertJSONMapEqual(t, src, em, got)
}

// assertJSONEqualsMap decodes MRI's JSON and compares to got.
func assertJSONEqualsMap(t *testing.T, src, wantJSON string, got *OrderedMap) {
	t.Helper()
	var want map[string]string
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatalf("decode MRI JSON %q: %v", wantJSON, err)
	}
	assertJSONMapEqual(t, src, want, got)
}

// assertJSONMapEqual compares an expected map to an OrderedMap's contents.
func assertJSONMapEqual(t *testing.T, src string, want map[string]string, got *OrderedMap) {
	t.Helper()
	if got.Len() != len(want) {
		t.Fatalf("Parse(%q) has %d keys, MRI has %d\n got=%v\nwant=%v",
			src, got.Len(), len(want), got.Map(), want)
	}
	for k, wv := range want {
		gv, ok := got.Get(k)
		if !ok {
			t.Fatalf("Parse(%q) missing key %q (MRI has it)", src, k)
		}
		if gv != wv {
			t.Fatalf("Parse(%q)[%q] = %q, MRI = %q", src, k, gv, wv)
		}
	}
}

// oracleEnvLookup mirrors the ambient ENV the oracle scripts set.
func oracleEnvLookup(key string) (string, bool) {
	if key == "ENVHOST" {
		return "env.example.com", true
	}
	return "", false
}
