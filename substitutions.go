// Copyright (c) the go-ruby-dotenv/dotenv authors
//
// SPDX-License-Identifier: BSD-3-Clause

package dotenv

import "strings"

// substituteVariable mirrors `Dotenv::Substitutions::Variable.call`, which applies
//
//	VARIABLE = /(\\)?(\$)(?!\()\{?([A-Z0-9_]+)?\}?/xi
//
// Go's RE2 has neither the `(?!\()` negative lookahead nor the exact greedy
// semantics needed here, so this is a hand-rolled scanner that reproduces the
// gem's gsub byte-for-byte:
//
//   - a `\$…` match with the leading backslash is "escaped": the backslash is
//     dropped and the rest kept verbatim (`variable[1..]`);
//   - a `$NAME` / `${NAME}` match with a name resolves against the accumulated
//     hash, then the ambient environment, else the empty string;
//   - a `$` with no following name (e.g. `$ `, `${}`) is left verbatim.
func (p *Parser) substituteVariable(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); {
		m := matchVariable(value, i)
		if m == nil {
			b.WriteByte(value[i])
			i++
			continue
		}
		switch {
		case m.escaped:
			// variable[1..] — drop the leading backslash, keep the rest.
			b.WriteString(value[m.start+1 : m.end])
		case m.name != "":
			b.WriteString(p.resolveVariable(m.name))
		default:
			// No name captured: keep the whole match verbatim.
			b.WriteString(value[m.start:m.end])
		}
		i = m.end
	}
	return b.String()
}

// resolveVariable resolves NAME to the .env hash value, then the ambient
// environment, else "" — mirroring `env[match[3]] || ENV[match[3]] || ""`.
func (p *Parser) resolveVariable(name string) string {
	if v, ok := p.hash.Get(name); ok {
		return v
	}
	if v, ok := p.lookup(name); ok {
		return v
	}
	return ""
}

// varMatch is one VARIABLE regex match: [start,end) over the source, whether it
// carried the leading escape backslash, and the captured NAME (possibly empty).
type varMatch struct {
	start, end int
	escaped    bool
	name       string
}

// matchVariable attempts the VARIABLE pattern anchored at s[i]. It returns nil if
// s[i] is not the start of a match (a lone `$` still matches with an empty name;
// only a non-`\`, non-`$` byte fails to start one).
func matchVariable(s string, i int) *varMatch {
	start := i
	escaped := false
	if s[i] == '\\' {
		// The backslash is optional; it only counts if a `$` follows it.
		if i+1 < len(s) && s[i+1] == '$' {
			escaped = true
			i++
		} else {
			return nil
		}
	}
	if i >= len(s) || s[i] != '$' {
		return nil
	}
	i++ // consume `$`
	// (?!\() — a `$` immediately followed by `(` is never a variable; the whole
	// VARIABLE regex fails at this position (the negative lookahead defeats even
	// the optional leading backslash), so no match — the bytes stay verbatim. This
	// is what preserves an escaped-but-unbalanced `\$(x` intact, backslash and all.
	if i < len(s) && s[i] == '(' {
		return nil
	}
	// \{? — optional opening brace.
	if i < len(s) && s[i] == '{' {
		i++
	}
	// ([A-Z0-9_]+)? — optional name, case-insensitive.
	nameStart := i
	for i < len(s) && isVarChar(s[i]) {
		i++
	}
	name := s[nameStart:i]
	// \}? — optional closing brace.
	if i < len(s) && s[i] == '}' {
		i++
	}
	return &varMatch{start: start, end: i, escaped: escaped, name: name}
}

// isVarChar reports whether c is in [A-Za-z0-9_] (the case-insensitive class).
func isVarChar(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
}

// substituteCommand mirrors `Dotenv::Substitutions::Command.call`, which applies
//
//	INTERPOLATED_SHELL_COMMAND = /(?<backslash>\\)?\$(\((?:[^()]|\g<cmd>)+\))/x
//
// i.e. `$(` … `)` with fully balanced parentheses (the recursive `\g<cmd>`). RE2
// cannot express recursion, so this hand-scans balanced parens.
//
// The gem executes the command (after inner variable expansion) and inlines the
// chomped output. Executing a shell is a HOST concern, so this parser always
// records each command (with its variable-expanded Script) in p.commands, and:
//
//   - with no runner wired, substitutes the empty string (the deterministic,
//     pure result); or
//   - with p.runCommand set (the host seam), invokes it inline and splices the
//     chomped output, so command→variable→command chains resolve as in MRI.
//
// An escaped `\$(...)` is not a command: the backslash is dropped and the rest
// (`$(...)`) kept verbatim, matching `$LAST_MATCH_INFO[0][1..]`.
func (p *Parser) substituteCommand(key, value string) string {
	var b strings.Builder
	for i := 0; i < len(value); {
		start, end, escaped, ok := matchCommand(value, i)
		if !ok {
			b.WriteByte(value[i])
			i++
			continue
		}
		if escaped {
			// `\$(...)` -> drop the backslash, keep `$(...)` verbatim.
			b.WriteString(value[start+1 : end])
		} else {
			// Inner variable expansion happens on the command text (the gem calls
			// Variable.call on it before shelling out). Strip the `$(` and `)`.
			inner := value[start+2 : end-1]
			script := p.substituteVariable(inner)
			p.commands = append(p.commands, Command{Key: key, Script: script})
			// With a host runner, execute inline and splice the chomped output (so
			// chains resolve); without one, the pure substitution is "".
			if p.runCommand != nil {
				b.WriteString(chomp(p.runCommand(script)))
			}
		}
		i = end
	}
	return b.String()
}

// chomp mirrors Ruby's `String#chomp`: it strips a single trailing record
// separator — `\r\n`, `\n`, or `\r` — matching the gem's “…`.chomp“.
func chomp(s string) string {
	switch {
	case strings.HasSuffix(s, "\r\n"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "\n"), strings.HasSuffix(s, "\r"):
		return s[:len(s)-1]
	}
	return s
}

// matchCommand attempts INTERPOLATED_SHELL_COMMAND anchored at s[i]. On success it
// returns the [start,end) span, whether it was escaped, and true. The command body
// must have at least one character and fully balanced parentheses.
func matchCommand(s string, i int) (start, end int, escaped bool, ok bool) {
	start = i
	if s[i] == '\\' {
		if i+1 < len(s) && s[i+1] == '$' {
			escaped = true
			i++
		} else {
			return 0, 0, false, false
		}
	}
	if i >= len(s) || s[i] != '$' {
		return 0, 0, false, false
	}
	i++ // `$`
	if i >= len(s) || s[i] != '(' {
		return 0, 0, false, false
	}
	// Scan a balanced-parenthesis group starting at the `(`.
	depth := 0
	j := i
	for ; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				// `(?:[^()]|\g<cmd>)+` requires at least one body char: `$()` (empty)
				// does not match, so require j > i+1.
				if j <= i+1 {
					return 0, 0, false, false
				}
				return start, j + 1, escaped, true
			}
		}
	}
	return 0, 0, false, false // unbalanced: no match
}
