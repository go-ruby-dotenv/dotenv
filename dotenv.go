// Copyright (c) the go-ruby-dotenv/dotenv authors
//
// SPDX-License-Identifier: BSD-3-Clause

package dotenv

// Env abstracts the ambient environment the gem reaches through `ENV`: a lookup
// (for existing-variable checks and `$VAR` interpolation) and a setter (what
// `Dotenv.load` performs after parsing). The whole struct is the HOST SEAM — a
// host wiring this to `os.LookupEnv` / `os.Setenv`, to a `go-embedded-ruby` ENV
// object, or to an in-memory map decides where mutation lands. Both fields are
// optional; a nil Lookup behaves like an empty environment and a nil Set makes
// Load a no-op mutation (it still returns the parsed pairs).
type Env struct {
	Lookup func(string) (string, bool)
	Set    func(key, val string)
	// RunCommand is the command-execution seam (the gem shells out for `$(cmd)`).
	// When set, ParseString/Load run it inline for each command and splice the
	// chomped output, so chains resolve as in MRI. When nil, commands yield "".
	RunCommand func(script string) string
}

// lookup adapts the Env (possibly nil) to a lookup function.
func (e *Env) lookup() func(string) (string, bool) {
	if e == nil {
		return nil
	}
	return e.Lookup
}

// runner adapts the Env (possibly nil) to a command runner.
func (e *Env) runner() func(string) string {
	if e == nil {
		return nil
	}
	return e.RunCommand
}

// ParseString parses src with the given Env, mirroring `Dotenv.parse` on a single
// source string (the gem's `Dotenv::Parser.call`). It does not mutate the
// environment. overwrite maps to the gem's `overwrite:` keyword. If the Env
// supplies a RunCommand, `$(cmd)` substitutions execute inline via that seam.
func ParseString(src string, overwrite bool, env *Env) (*OrderedMap, error) {
	m, _, err := ParseWithRunner(src, overwrite, env.lookup(), env.runner())
	return m, err
}

// Load parses src and then sets each parsed pair into the environment via env.Set,
// mirroring `Dotenv.load` (parse + update ENV). It honours the gem's rule that a
// key already present in the environment is not overwritten unless overwrite is
// true (the parse already resolves such keys to their existing value). The mutation
// is entirely the host's Env.Set seam; the parsed OrderedMap is returned so the
// host can inspect exactly what was applied. Parsed `$(command)` substitutions are
// available via the returned Commands, since executing a shell is a host concern.
func Load(src string, overwrite bool, env *Env) (*OrderedMap, []Command, error) {
	hash, cmds, err := ParseWithRunner(src, overwrite, env.lookup(), env.runner())
	if err != nil {
		return nil, nil, err
	}
	if env != nil && env.Set != nil {
		hash.Each(func(key, val string) {
			// Do not clobber an existing value unless overwriting (the gem's
			// update semantics); when overwrite is true, always set.
			if overwrite {
				env.Set(key, val)
				return
			}
			if env.Lookup != nil {
				if _, ok := env.Lookup(key); ok {
					return
				}
			}
			env.Set(key, val)
		})
	}
	return hash, cmds, nil
}
