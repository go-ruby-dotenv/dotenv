// Copyright (c) the go-ruby-dotenv/dotenv authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package dotenv is a pure-Go (CGO=0) reimplementation of Ruby's `dotenv` gem
// (bkeepers/dotenv 3.2.0) `.env` file parser — the deterministic,
// interpreter-independent core of `Dotenv::Parser.call`. It parses the `.env`
// format into an insertion-ordered set of key/value pairs, byte-faithful to MRI,
// with variable interpolation and command-substitution parsing, but WITHOUT any
// Ruby runtime and WITHOUT touching the process environment.
//
// The pieces that are genuinely a host concern — mutating `ENV` (what
// `Dotenv.load` does after parsing) and executing `$(command)` substitutions
// (which the gem does by shelling out) — are surfaced as explicit seams rather
// than performed here, keeping the package pure compute.
package dotenv

import (
	"fmt"
	"regexp"
	"strings"
)

// FormatError is raised when a line declares an exported variable with no value
// and no prior assignment, mirroring Ruby's `Dotenv::FormatError`.
type FormatError struct{ Line string }

func (e *FormatError) Error() string {
	return fmt.Sprintf("Line %q has an unset variable", e.Line)
}

// lineRE mirrors the gem's `Dotenv::Parser::LINE` (an /x extended regex). Each
// scan match yields the optional `export`, the `key`, and the optional `value`.
// RE2 has no backreferences here so it ports verbatim; the value's own quote
// removal is done afterwards (see quotedRE / parseValue).
//
//	(?:^|\A)                # beginning of line
//	\s*                     # leading whitespace
//	(?<export>export\s+)?   # optional export
//	(?<key>[\w.]+)          # key
//	(?:                     # optional separator and value
//	  (?:\s*=\s*?|:\s+?)    #   separator
//	  (?<value>             #   optional value begin
//	    \s*'(?:\\'|[^'])*'  #     single quoted value
//	    |                   #     or
//	    \s*"(?:\\"|[^"])*"  #     double quoted value
//	    |                   #     or
//	    [^\#\n]+            #     unquoted value
//	  )?                    #   value end
//	)?                      # separator and value end
//	\s*                     # trailing whitespace
//	(?:\#.*)?               # optional comment
//	(?:$|\z)                # end of line
var lineRE = regexp.MustCompile(`(?m)(?:^|\A)\s*(export\s+)?([\w.]+)(?:(?:\s*=\s*?|:\s+?)(\s*'(?:\\'|[^'])*'|\s*"(?:\\"|[^"])*"|[^#\n]+)?)?\s*(?:#.*)?(?:$|\z)`)

// The gem's `QUOTED_STRING` — /\A(['"])(.*)\1\z/m — strips a matching pair of
// surrounding quotes. RE2 lacks the `\1` backreference, so stripQuotes (value.go)
// implements the equivalent directly.

// Parser holds the state of a single `.env` parse: the (line-normalised) source,
// the accumulated ordered hash, the overwrite flag, and the environment lookup
// seam standing in for MRI's `ENV`.
type Parser struct {
	source    string
	hash      *OrderedMap
	overwrite bool
	// lookupEnv resolves a name against the ambient environment (the role of
	// `ENV` in the gem). It returns the value and whether the name is present.
	// A nil lookupEnv behaves like an empty environment.
	lookupEnv func(string) (string, bool)
	// runCommand is the command-execution seam. The gem shells out to run a
	// `$(command)` and inlines the chomped output; executing a shell is a host
	// concern, so this func stands in for it. When nil, a command substitutes the
	// empty string (the deterministic pure result) and is only recorded. When
	// supplied by the host, it is invoked inline during parsing with the
	// variable-expanded script and its return value is chomped and spliced in —
	// which is what makes command-to-variable-to-command chains resolve exactly as
	// MRI does. Every command is recorded in commands either way.
	runCommand func(script string) string
	// commands collects every parsed `$(command)` substitution so the host can
	// execute or audit them. See Command.
	commands []Command
}

// Command is a parsed `$(...)` shell-command substitution the gem would execute.
// The parser records each one (with the fully variable-expanded Script the gem
// would pass to the shell) and substitutes the empty string in its place, leaving
// actual execution to the host.
type Command struct {
	// Key is the .env key whose value contained the substitution.
	Key string
	// Script is the command text after inner `$VAR`/`${VAR}` expansion, exactly
	// what the gem passes to the shell (its result is chomped and inlined).
	Script string
}

// Parse parses the `.env`-format src into an OrderedMap, byte-faithful to
// `Dotenv::Parser.call`.
//
//   - overwrite mirrors the gem's `overwrite:` keyword. When false, a key already
//     present in the ambient environment (per lookupEnv) keeps that ambient value
//     — except "DOTENV_LINEBREAK_MODE", which is always taken from the file.
//   - lookupEnv stands in for `ENV`: it resolves existing-variable checks and
//     `$VAR` interpolation fallbacks. Pass nil for an empty environment.
//
// It returns a FormatError if an `export KEY` line names a variable that was
// never assigned. Any `$(command)` substitutions are parsed, expanded, replaced
// by "" in the value, and returned via Commands for the host to execute.
func Parse(src string, overwrite bool, lookupEnv func(string) (string, bool)) (*OrderedMap, error) {
	p := newParser(src, overwrite, lookupEnv, nil)
	if err := p.run(); err != nil {
		return nil, err
	}
	return p.hash, nil
}

// ParseWithCommands is Parse but also returns the parsed `$(command)`
// substitutions (the host-execution seam) in source order. With no runner every
// command substitutes "" (the pure result); to have chains resolve, supply a
// command runner via ParseWithRunner.
func ParseWithCommands(src string, overwrite bool, lookupEnv func(string) (string, bool)) (*OrderedMap, []Command, error) {
	p := newParser(src, overwrite, lookupEnv, nil)
	if err := p.run(); err != nil {
		return nil, nil, err
	}
	return p.hash, p.commands, nil
}

// ParseWithRunner is Parse with the command-execution seam wired: runCommand is
// invoked inline during parsing for each `$(command)` (with the variable-expanded
// script), and its chomped return value is spliced into the value — so a command
// feeding a later variable or command resolves exactly as MRI's shelling-out does.
// A nil runCommand behaves like Parse (commands become ""). It also returns the
// recorded commands.
func ParseWithRunner(src string, overwrite bool, lookupEnv func(string) (string, bool), runCommand func(script string) string) (*OrderedMap, []Command, error) {
	p := newParser(src, overwrite, lookupEnv, runCommand)
	if err := p.run(); err != nil {
		return nil, nil, err
	}
	return p.hash, p.commands, nil
}

// newParser builds a Parser with the CRLF/CR line-ending normalisation the gem
// applies (`gsub(/\r\n?/, "\n")`).
func newParser(src string, overwrite bool, lookupEnv func(string) (string, bool), runCommand func(string) string) *Parser {
	return &Parser{
		source:     strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(src),
		hash:       NewOrderedMap(),
		overwrite:  overwrite,
		lookupEnv:  lookupEnv,
		runCommand: runCommand,
	}
}

// run drives the scan, mirroring `Dotenv::Parser#call`. It uses submatch indices
// so it can tell an unset value group (gem: `match[:value]` is nil) from an empty
// one — the distinction the `export KEY` error path turns on.
func (p *Parser) run() error {
	// Group indices in lineRE: 0=whole match, 1=export, 2=key, 3=value.
	for _, loc := range lineRE.FindAllStringSubmatchIndex(p.source, -1) {
		whole := group(p.source, loc, 0)
		export := group(p.source, loc, 1)
		key := group(p.source, loc, 2)
		value, valueSet := groupOpt(p.source, loc, 3)

		switch {
		case p.existing(key):
			// Use value from an already-defined ambient variable.
			v, _ := p.lookup(key)
			p.hash.Set(key, v)
		case export != "" && !valueSet:
			// `export KEY` with no value: legal only if KEY was already assigned.
			if !p.hash.Has(key) {
				return &FormatError{Line: whole}
			}
		default:
			p.hash.Set(key, p.parseValue(key, value))
		}
	}
	return nil
}

// group returns capture group n's text (empty if the group did not participate).
func group(s string, loc []int, n int) string {
	i, j := loc[2*n], loc[2*n+1]
	if i < 0 {
		return ""
	}
	return s[i:j]
}

// groupOpt returns capture group n's text and whether the group participated in
// the match (mirroring a non-nil `match[:value]`).
func groupOpt(s string, loc []int, n int) (string, bool) {
	i, j := loc[2*n], loc[2*n+1]
	if i < 0 {
		return "", false
	}
	return s[i:j], true
}

// existing mirrors `Dotenv::Parser#existing?`: a key that must not be overwritten
// because it is already set in the ambient environment (and is not the
// linebreak-mode control key).
func (p *Parser) existing(key string) bool {
	if p.overwrite || key == "DOTENV_LINEBREAK_MODE" {
		return false
	}
	_, ok := p.lookup(key)
	return ok
}

// lookup resolves a name via the environment seam, tolerating a nil lookupEnv.
func (p *Parser) lookup(key string) (string, bool) {
	if p.lookupEnv == nil {
		return "", false
	}
	return p.lookupEnv(key)
}
