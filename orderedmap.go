// Copyright (c) the go-ruby-dotenv/dotenv authors
//
// SPDX-License-Identifier: BSD-3-Clause

package dotenv

// OrderedMap is an insertion-ordered string->string map — the Go analogue of the
// Ruby `Hash` that `Dotenv::Parser.call` returns. Ruby hashes preserve insertion
// order, and dotenv relies on it (later keys may interpolate earlier ones), so the
// order is part of the observable, byte-faithful result.
type OrderedMap struct {
	keys   []string
	values map[string]string
}

// NewOrderedMap returns an empty OrderedMap.
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{values: map[string]string{}}
}

// Set assigns key=val, appending the key on first insertion and preserving its
// original position on update (Ruby Hash#[]= semantics).
func (m *OrderedMap) Set(key, val string) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = val
}

// Get returns the value for key and whether it is present.
func (m *OrderedMap) Get(key string) (string, bool) {
	v, ok := m.values[key]
	return v, ok
}

// Has reports whether key is present (Ruby Hash#member?).
func (m *OrderedMap) Has(key string) bool {
	_, ok := m.values[key]
	return ok
}

// Len returns the number of pairs.
func (m *OrderedMap) Len() int { return len(m.keys) }

// Keys returns the keys in insertion order (a copy; safe to mutate).
func (m *OrderedMap) Keys() []string {
	out := make([]string, len(m.keys))
	copy(out, m.keys)
	return out
}

// Map returns a plain (unordered) map copy of the pairs.
func (m *OrderedMap) Map() map[string]string {
	out := make(map[string]string, len(m.keys))
	for k, v := range m.values {
		out[k] = v
	}
	return out
}

// Each calls fn for every pair in insertion order.
func (m *OrderedMap) Each(fn func(key, val string)) {
	for _, k := range m.keys {
		fn(k, m.values[k])
	}
}
