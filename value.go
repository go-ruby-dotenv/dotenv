// Copyright (c) the go-ruby-dotenv/dotenv authors
//
// SPDX-License-Identifier: BSD-3-Clause

package dotenv

import (
	"regexp"
	"strings"
)

// unescapeRE mirrors the gem's `unescape_characters` — `value.gsub(/\\([^$])/, '\1')`.
// It drops a backslash before any character except `$` (so `\$` survives into the
// variable substitution pass, where `\$` means "escaped, literal `$`").
var unescapeRE = regexp.MustCompile(`\\([^$])`)

// parseValue mirrors `Dotenv::Parser#parse_value`. It strips a matching pair of
// surrounding quotes; for a double-quoted value it applies the linebreak
// expansion; and for anything not single-quoted it unescapes characters and then
// runs the substitutions (Command first, then Variable — the gem's order).
func (p *Parser) parseValue(key, value string) string {
	// Remove surrounding quotes: value.strip.sub(QUOTED_STRING, '\2').
	stripped := strings.TrimSpace(value)
	inner, quote := stripQuotes(stripped)
	value = inner

	// Expand new lines in double-quoted values.
	if quote == '"' {
		value = p.expandNewlines(value)
	}

	// Unescape characters and perform substitutions unless single-quoted.
	if quote != '\'' {
		value = unescapeRE.ReplaceAllString(value, "$1")
		value = p.substituteCommand(key, value)
		value = p.substituteVariable(value)
	}
	return value
}

// stripQuotes mirrors QUOTED_STRING = /\A(['"])(.*)\1\z/m applied via sub: if s is
// wrapped in a matching pair of single or double quotes (the `m` flag lets the
// inner `.*` span newlines, which byte-wise it always does here), it returns the
// inner text and the quote byte; otherwise it returns s unchanged and a zero quote
// byte. The pattern needs the whole string to be a single quoted span, so s must
// be at least two bytes with equal first/last quote.
func stripQuotes(s string) (string, byte) {
	if len(s) >= 2 {
		q := s[0]
		if (q == '\'' || q == '"') && s[len(s)-1] == q {
			return s[1 : len(s)-1], q
		}
	}
	return s, 0
}

// expandNewlines mirrors `Dotenv::Parser#expand_newlines`. In the default
// (non-legacy) mode a literal `\n` / `\r` in a double-quoted value is turned into
// a backslash followed by the letter (`\n` -> `\` + `n`), matching the gem's
// `gsub('\n', "\\\\\\n")`; in legacy mode they become real newline / carriage
// return. The mode is read from the file's own DOTENV_LINEBREAK_MODE (already
// parsed) or the ambient environment.
func (p *Parser) expandNewlines(value string) string {
	mode := ""
	if v, ok := p.hash.Get("DOTENV_LINEBREAK_MODE"); ok {
		mode = v
	} else if v, ok := p.lookup("DOTENV_LINEBREAK_MODE"); ok {
		mode = v
	}
	if mode == "legacy" {
		value = strings.ReplaceAll(value, `\n`, "\n")
		value = strings.ReplaceAll(value, `\r`, "\r")
		return value
	}
	// Default mode: `\n` -> `\\n`, `\r` -> `\\r` (add one backslash), which the
	// subsequent unescape pass reduces back to a literal `\n` / `\r`.
	value = strings.ReplaceAll(value, `\n`, `\\n`)
	value = strings.ReplaceAll(value, `\r`, `\\r`)
	return value
}
