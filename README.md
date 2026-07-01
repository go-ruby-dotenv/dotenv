<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-dotenv/brand/main/social/go-ruby-dotenv-dotenv.png" alt="go-ruby-dotenv/dotenv" width="720"></p>

# dotenv — go-ruby-dotenv

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-dotenv.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of Ruby's [`dotenv`](https://github.com/bkeepers/dotenv)
gem's `.env` parser** — the deterministic, interpreter-independent core of
`Dotenv::Parser.call` (dotenv 3.2.0). It parses the `.env` file format into an
insertion-ordered set of key/value pairs, byte-faithful to MRI, with full variable
interpolation and command-substitution parsing — **without any Ruby runtime and
without touching the process environment**.

It is the dotenv backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module with no dependency on the Ruby runtime — a sibling
of [go-ruby-yaml](https://github.com/go-ruby-yaml/yaml) and
[go-ruby-erb](https://github.com/go-ruby-erb/erb).

> **What it is — and isn't.** Parsing the `.env` grammar — the `LINE` regex, quote
> stripping, escape handling, `$VAR` / `${VAR}` interpolation, and the `$(command)`
> grammar — is fully deterministic and needs **no interpreter**, so it lives here as
> pure Go. The two genuinely host-side pieces are surfaced as explicit seams, not
> performed here:
>
> - **Mutating `ENV`** — what `Dotenv.load` does after parsing — is the host's job
>   via `Env.Set`.
> - **Executing `$(command)`** — the gem shells out — is the host's job via
>   `Env.RunCommand`. With no runner, a command parses to the empty string and is
>   *recorded* (key + variable-expanded script) for the host to run; with a runner,
>   it executes inline so `command → variable → command` chains resolve exactly as
>   MRI's shelling-out does.

## Features

Faithful port of `Dotenv::Parser`, validated against the `dotenv` gem on every
supported platform:

- **Every value form** — unquoted (trailing whitespace and inline `#` comments
  trimmed), single-quoted (literal, no interpolation), and double-quoted (with the
  `\n \t \r \"` escapes and `$VAR` / `${VAR}` interpolation).
- **Line grammar** — `KEY=value`, `export KEY=value`, YAML-style `KEY: value`,
  `KEY=` (empty), bare `KEY`, blank lines, full-line and inline `#` comments, keys
  with `.` in them, and a leading UTF-8 BOM.
- **Variable interpolation** — `$VAR` / `${VAR}` referencing earlier keys then the
  ambient environment, `\$` to escape it, and the exact brace / empty-name /
  case-insensitive edge cases of the gem's `VARIABLE` regex.
- **Command substitution** — the `$(command)` grammar with fully balanced nested
  parentheses, inner variable expansion, and `\$(...)` escaping; execution is a
  host seam (see above).
- **Multiline** quoted values spanning newlines, `\r\n` / `\r` normalisation, and
  the default / `legacy` `DOTENV_LINEBREAK_MODE` `\n` / `\r` expansion.
- **`overwrite`** semantics — a key already set in the environment keeps its value
  unless overwriting (with `DOTENV_LINEBREAK_MODE` exempt), and the `export KEY`
  unset-variable `FormatError`.

CGO-free, dependency-free, **100% test coverage**, `gofmt` + `go vet` clean, and
green across the six 64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le,
s390x).

## Install

```sh
go get github.com/go-ruby-dotenv/dotenv
```

## Usage

```go
package main

import (
	"fmt"
	"os"

	"github.com/go-ruby-dotenv/dotenv"
)

func main() {
	src := `HOST=example.com
export URL="https://$HOST/api"
# a comment
DEBUG=true`

	// Pure parse (Dotenv::Parser.call). No env is read or written.
	h, err := dotenv.Parse(src, false, os.LookupEnv)
	if err != nil {
		panic(err)
	}
	u, _ := h.Get("URL")
	fmt.Println(u) // https://example.com/api

	// Load semantics (Dotenv.load): parse + set into the environment via seams.
	env := &dotenv.Env{Lookup: os.LookupEnv, Set: func(k, v string) { os.Setenv(k, v) }}
	dotenv.Load(src, false, env)
}
```

## Command substitution seam

The gem runs `$(command)` by shelling out. Here that is a host seam:

```go
// Record only (pure): value becomes "", commands are returned for the host.
h, cmds, _ := dotenv.ParseWithCommands("SHA=$(git rev-parse HEAD)", false, nil)
// cmds[0] == dotenv.Command{Key: "SHA", Script: "git rev-parse HEAD"}

// Execute inline via a runner (chains resolve as in MRI):
run := func(script string) string {
	out, _ := exec.Command("/bin/sh", "-c", script).Output()
	return string(out) // chomped by the parser
}
h, _, _ = dotenv.ParseWithRunner("FOO=$(echo bar)\nBAR=$(echo $FOO)", false, nil, run)
// h["FOO"] == "bar", h["BAR"] == "bar"
```

## API

```go
// Parse is Dotenv::Parser.call: parse src into an insertion-ordered map.
// overwrite maps to the gem's overwrite: keyword; lookupEnv stands in for ENV
// (nil = empty). $(command) substitutions yield "".
func Parse(src string, overwrite bool, lookupEnv func(string) (string, bool)) (*OrderedMap, error)

// ParseWithCommands also returns the parsed $(command) substitutions (Script is
// the variable-expanded command text the gem would run).
func ParseWithCommands(src string, overwrite bool, lookupEnv func(string) (string, bool)) (*OrderedMap, []Command, error)

// ParseWithRunner wires the command-execution seam: runCommand runs inline and
// its chomped output is spliced in, so chains resolve.
func ParseWithRunner(src string, overwrite bool, lookupEnv func(string) (string, bool), runCommand func(script string) string) (*OrderedMap, []Command, error)

// ParseString / Load use the Env seam (Lookup / Set / RunCommand). Load mutates
// via Env.Set (Dotenv.load) and returns the parsed pairs + commands.
func ParseString(src string, overwrite bool, env *Env) (*OrderedMap, error)
func Load(src string, overwrite bool, env *Env) (*OrderedMap, []Command, error)

type Env struct {
	Lookup     func(string) (string, bool) // stands in for ENV reads
	Set        func(key, val string)       // Dotenv.load's ENV write (host seam)
	RunCommand func(script string) string  // $(command) execution (host seam)
}

type Command struct { Key, Script string }     // a parsed $(command)
type FormatError struct { Line string }        // export KEY with no value

type OrderedMap struct { /* insertion-ordered string->string */ }
func NewOrderedMap() *OrderedMap
func (m *OrderedMap) Set(key, val string)
func (m *OrderedMap) Get(key string) (string, bool)
func (m *OrderedMap) Has(key string) bool
func (m *OrderedMap) Len() int
func (m *OrderedMap) Keys() []string
func (m *OrderedMap) Map() map[string]string
func (m *OrderedMap) Each(fn func(key, val string))
```

## Tests & coverage

The suite pairs deterministic, ruby-free **golden** tests (a corpus captured from
the real `dotenv` gem, `testdata/golden.json`, embedded via `go:embed`) with a
live **differential MRI oracle** (`oracle_test.go`) that re-derives the same corpus
against the system `ruby` + `dotenv` gem — gated on `RUBY_VERSION >= "4.0"` — and
also confirms the `overwrite` path and that the recorded command scripts, when run,
reproduce MRI's inlined results. The golden tests alone hold coverage at **100%**,
so the qemu cross-arch and Windows lanes (where the oracle skips itself) still pass
the gate.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-dotenv/dotenv authors.
