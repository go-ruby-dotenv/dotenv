// Copyright (c) the go-ruby-dotenv/dotenv authors
//
// SPDX-License-Identifier: BSD-3-Clause

package dotenv

import (
	"reflect"
	"testing"
)

// mapFromKV builds an env-lookup func from alternating key,value pairs.
func envFrom(kv ...string) func(string) (string, bool) {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestExistingAndOverwrite(t *testing.T) {
	// existing?: with overwrite=false a key already in the environment keeps its
	// ambient value; with overwrite=true the file value wins.
	env := envFrom("FOO", "existing")
	got, err := Parse("FOO=bar", false, env)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.Get("FOO"); v != "existing" {
		t.Fatalf("no-overwrite FOO = %q, want existing", v)
	}
	got, _ = Parse("FOO=bar", true, env)
	if v, _ := got.Get("FOO"); v != "bar" {
		t.Fatalf("overwrite FOO = %q, want bar", v)
	}
	// DOTENV_LINEBREAK_MODE is exempt from the existing? guard: even if present in
	// the environment, the file value is taken.
	env2 := envFrom("DOTENV_LINEBREAK_MODE", "strict")
	got, _ = Parse("DOTENV_LINEBREAK_MODE=legacy", false, env2)
	if v, _ := got.Get("DOTENV_LINEBREAK_MODE"); v != "legacy" {
		t.Fatalf("linebreak-mode key = %q, want legacy (exempt)", v)
	}
}

func TestNilLookup(t *testing.T) {
	// A nil lookupEnv behaves like an empty environment: no existing keys, and
	// $VAR falls back to "".
	got, err := Parse("A=1\nB=$UNSET", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.Get("B"); v != "" {
		t.Fatalf("B = %q, want empty", v)
	}
}

func TestFormatError(t *testing.T) {
	_, err := Parse("export NOPE", false, nil)
	if err == nil {
		t.Fatal("want FormatError for unset exported var")
	}
	fe, ok := err.(*FormatError)
	if !ok {
		t.Fatalf("err type = %T, want *FormatError", err)
	}
	if got := fe.Error(); got != `Line "export NOPE" has an unset variable` {
		t.Fatalf("error = %q", got)
	}
	// `export KEY` after KEY was assigned is legal.
	got, err := Parse("KEY=1\nexport KEY", false, nil)
	if err != nil {
		t.Fatalf("legal export re-declaration errored: %v", err)
	}
	if v, _ := got.Get("KEY"); v != "1" {
		t.Fatalf("KEY = %q, want 1", v)
	}
}

func TestLegacyLinebreakMode(t *testing.T) {
	// In-file DOTENV_LINEBREAK_MODE=legacy turns \n and \r into real breaks.
	got, err := Parse("DOTENV_LINEBREAK_MODE=legacy\nFOO=\"a\\nb\\rc\"", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.Get("FOO"); v != "a\nb\rc" {
		t.Fatalf("legacy FOO = %q, want a\\nb\\rc real breaks", v)
	}
	// Legacy mode via the ambient environment.
	got, _ = Parse("FOO=\"x\\ny\"", false, envFrom("DOTENV_LINEBREAK_MODE", "legacy"))
	if v, _ := got.Get("FOO"); v != "x\ny" {
		t.Fatalf("env-legacy FOO = %q", v)
	}
}

func TestCommandSubstitutionSeam(t *testing.T) {
	// $(...) yields "" (the pure result) and records the variable-expanded script.
	h, cmds, err := ParseWithCommands("A=$(echo hi)\nB=$(echo $A)", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := h.Get("A"); v != "" {
		t.Fatalf("A = %q, want empty (host executes)", v)
	}
	if len(cmds) != 2 {
		t.Fatalf("recorded %d commands, want 2", len(cmds))
	}
	if cmds[0].Key != "A" || cmds[0].Script != "echo hi" {
		t.Fatalf("cmd[0] = %+v", cmds[0])
	}
	// Inner $A expands against the accumulated hash before the host would run it.
	if cmds[1].Script != "echo " {
		t.Fatalf("cmd[1].Script = %q, want %q", cmds[1].Script, "echo ")
	}
	// Escaped \$(...) is not a command: backslash dropped, rest literal.
	h, cmds, _ = ParseWithCommands(`E=esc-\$(echo x)`, false, nil)
	if len(cmds) != 0 {
		t.Fatalf("escaped form recorded %d commands, want 0", len(cmds))
	}
	if v, _ := h.Get("E"); v != "esc-$(echo x)" {
		t.Fatalf("escaped value = %q", v)
	}
	// Nested balanced parens captured whole.
	_, cmds, _ = ParseWithCommands(`N=$(echo "$(echo hi)")`, false, nil)
	if len(cmds) != 1 || cmds[0].Script != `echo "$(echo hi)"` {
		t.Fatalf("nested cmd = %+v", cmds)
	}
}

func TestEmptyAndUnbalancedCommand(t *testing.T) {
	// $() (empty body) is not a command match; it stays literal.
	h, cmds, _ := ParseWithCommands("K=$()", false, nil)
	if len(cmds) != 0 {
		t.Fatalf("empty $() recorded a command")
	}
	if v, _ := h.Get("K"); v != "$()" {
		t.Fatalf("K = %q, want literal $()", v)
	}
	// Unbalanced $( ... with no closing paren stays literal.
	h, cmds, _ = ParseWithCommands("K=$(oops", false, nil)
	if len(cmds) != 0 {
		t.Fatalf("unbalanced recorded a command")
	}
	if v, _ := h.Get("K"); v != "$(oops" {
		t.Fatalf("K = %q, want literal", v)
	}
}

func TestBackslashNotDollar(t *testing.T) {
	// A `\` not before `$` in the substitution passes are left for the earlier
	// unescape pass; by substitution time e.g. `\x` is already `x`. Feed a value
	// through Parse to confirm a lone backslash inside quotes is handled.
	got, _ := Parse(`K="a\\b"`, false, nil)
	if v, _ := got.Get("K"); v != `a\b` {
		t.Fatalf("K = %q, want a\\b", v)
	}
	// Trailing backslash unquoted.
	got, _ = Parse(`K=a\`, false, nil)
	if v, _ := got.Get("K"); v != `a\` {
		t.Fatalf("trailing-backslash K = %q", v)
	}
}

func TestParseStringAndLoad(t *testing.T) {
	// ParseString delegates to Parse via the Env seam (no mutation).
	got, err := ParseString("A=1\nB=$A", false, &Env{Lookup: envFrom()})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.Get("B"); v != "1" {
		t.Fatalf("B = %q, want 1", v)
	}
	// nil Env still works.
	if _, err := ParseString("A=1", false, nil); err != nil {
		t.Fatal(err)
	}

	// Load mutates via Env.Set, honouring existing keys unless overwrite.
	store := map[string]string{"KEEP": "old"}
	env := &Env{
		Lookup: func(k string) (string, bool) { v, ok := store[k]; return v, ok },
		Set:    func(k, v string) { store[k] = v },
	}
	if _, _, err := Load("KEEP=new\nADD=1", false, env); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store, map[string]string{"KEEP": "old", "ADD": "1"}) {
		t.Fatalf("no-overwrite store = %v", store)
	}
	if _, _, err := Load("KEEP=new2", true, env); err != nil {
		t.Fatal(err)
	}
	if store["KEEP"] != "new2" {
		t.Fatalf("overwrite store KEEP = %q, want new2", store["KEEP"])
	}

	// Load with a nil Set is a no-op mutation but still returns the hash.
	h, _, err := Load("X=1", false, &Env{Lookup: envFrom()})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := h.Get("X"); v != "1" {
		t.Fatalf("nil-Set Load X = %q", v)
	}
	// Load with a nil Env is a no-op mutation.
	if _, _, err := Load("X=1", false, nil); err != nil {
		t.Fatal(err)
	}
	// Load without a Lookup but with Set: sets unconditionally on no-overwrite.
	store2 := map[string]string{}
	if _, _, err := Load("Y=2", false, &Env{Set: func(k, v string) { store2[k] = v }}); err != nil {
		t.Fatal(err)
	}
	if store2["Y"] != "2" {
		t.Fatalf("no-lookup Set store = %v", store2)
	}
	// Load surfaces a FormatError from the parse.
	if _, _, err := Load("export NOPE", false, nil); err == nil {
		t.Fatal("Load should surface FormatError")
	}
}

func TestCommandRunnerSeam(t *testing.T) {
	// A runner executes commands inline; the chomped output is spliced, so a
	// command feeding a later variable/command chains correctly.
	run := func(script string) string {
		switch script {
		case "get-foo":
			return "bar\n" // trailing newline is chomped
		case "echo bar":
			return "bar"
		default:
			return "?" + script
		}
	}
	got, cmds, err := ParseWithRunner("FOO=$(get-foo)\nBAR=$(echo $FOO)", false, nil, run)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.Get("FOO"); v != "bar" {
		t.Fatalf("FOO = %q, want bar (chomped)", v)
	}
	if v, _ := got.Get("BAR"); v != "bar" {
		t.Fatalf("BAR = %q, want bar (chain resolved)", v)
	}
	if len(cmds) != 2 {
		t.Fatalf("recorded %d commands, want 2", len(cmds))
	}
	// chomp variants.
	for in, want := range map[string]string{"a\r\n": "a", "b\r": "b", "c\n": "c", "d": "d", "": ""} {
		got, _, _ := ParseWithRunner("K=$(x)", false, nil, func(string) string { return in })
		if v, _ := got.Get("K"); v != want {
			t.Fatalf("chomp(%q) spliced = %q, want %q", in, v, want)
		}
	}
	// Env.RunCommand wires the seam through ParseString and Load.
	env := &Env{RunCommand: func(string) string { return "ok" }}
	m, err := ParseString("K=$(anything)", false, env)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := m.Get("K"); v != "ok" {
		t.Fatalf("ParseString runner K = %q", v)
	}
	store := map[string]string{}
	env2 := &Env{RunCommand: func(string) string { return "run" }, Set: func(k, v string) { store[k] = v }}
	if _, _, err := Load("K=$(x)", false, env2); err != nil {
		t.Fatal(err)
	}
	if store["K"] != "run" {
		t.Fatalf("Load runner store = %v", store)
	}
}

func TestParseWithCommandsError(t *testing.T) {
	// ParseWithCommands surfaces the parse error.
	if _, _, err := ParseWithCommands("export NOPE", false, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestOrderedMap(t *testing.T) {
	m := NewOrderedMap()
	m.Set("a", "1")
	m.Set("b", "2")
	m.Set("a", "3") // update keeps position
	if m.Len() != 2 {
		t.Fatalf("Len = %d, want 2", m.Len())
	}
	if !reflect.DeepEqual(m.Keys(), []string{"a", "b"}) {
		t.Fatalf("Keys = %v", m.Keys())
	}
	if v, ok := m.Get("a"); !ok || v != "3" {
		t.Fatalf("Get(a) = %q,%v", v, ok)
	}
	if _, ok := m.Get("z"); ok {
		t.Fatal("Get(z) should be absent")
	}
	if !m.Has("b") || m.Has("z") {
		t.Fatal("Has wrong")
	}
	if !reflect.DeepEqual(m.Map(), map[string]string{"a": "3", "b": "2"}) {
		t.Fatalf("Map = %v", m.Map())
	}
	var order []string
	m.Each(func(k, v string) { order = append(order, k+"="+v) })
	if !reflect.DeepEqual(order, []string{"a=3", "b=2"}) {
		t.Fatalf("Each order = %v", order)
	}
}
