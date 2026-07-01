// Copyright (c) the go-ruby-dotenv/dotenv authors
//
// SPDX-License-Identifier: BSD-3-Clause

package dotenv

import (
	_ "embed"
	"encoding/json"
	"testing"
)

// goldenJSON is the differential corpus captured from the real `dotenv` gem
// (bkeepers/dotenv 3.2.0) — see testdata/golden.json. Each entry maps a `.env`
// source string to the exact Hash the gem's `Dotenv::Parser.call` returns, or to
// {"__error__": msg} for a source the gem rejects. These deterministic vectors
// hold coverage at 100% with no Ruby present, so the qemu cross-arch and Windows
// CI lanes pass the gate; oracle_test.go re-derives them live against MRI.
//
//go:embed testdata/golden.json
var goldenJSON []byte

// goldenEnv is the ambient environment the golden corpus was captured under: only
// ENVHOST is defined, so `$ENVHOST` interpolates and every other name falls back
// to the empty string.
func goldenEnv(key string) (string, bool) {
	if key == "ENVHOST" {
		return "env.example.com", true
	}
	return "", false
}

func TestGoldenCorpus(t *testing.T) {
	var golden map[string]json.RawMessage
	if err := json.Unmarshal(goldenJSON, &golden); err != nil {
		t.Fatalf("decode golden.json: %v", err)
	}
	if len(golden) == 0 {
		t.Fatal("golden corpus is empty")
	}
	for src, expRaw := range golden {
		t.Run(shortName(src), func(t *testing.T) {
			var em map[string]string
			if err := json.Unmarshal(expRaw, &em); err != nil {
				t.Fatalf("decode expected: %v", err)
			}
			got, err := Parse(src, false, goldenEnv)
			if msg, isErr := em["__error__"]; isErr && len(em) == 1 {
				if err == nil {
					t.Fatalf("Parse(%q) = no error, want FormatError %q", src, msg)
				}
				if err.Error() != msg {
					t.Fatalf("Parse(%q) error = %q, want %q", src, err.Error(), msg)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", src, err)
			}
			assertMapEqual(t, src, got, em)
		})
	}
}

// assertMapEqual checks got equals the expected key/value set exactly.
func assertMapEqual(t *testing.T, src string, got *OrderedMap, want map[string]string) {
	t.Helper()
	if got.Len() != len(want) {
		t.Fatalf("Parse(%q) has %d keys, want %d\n got=%v\nwant=%v",
			src, got.Len(), len(want), got.Map(), want)
	}
	for k, wv := range want {
		gv, ok := got.Get(k)
		if !ok {
			t.Fatalf("Parse(%q) missing key %q", src, k)
		}
		if gv != wv {
			t.Fatalf("Parse(%q)[%q] = %q, want %q", src, k, gv, wv)
		}
	}
}

// shortName makes a test-safe subtest name from a source string.
func shortName(s string) string {
	if s == "" {
		return "empty"
	}
	const max = 24
	out := make([]rune, 0, max)
	for _, r := range s {
		switch r {
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		case ' ', '/':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
		if len(out) >= max {
			out = append(out, '~')
			break
		}
	}
	return string(out)
}
