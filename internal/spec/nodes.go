package spec

import (
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// decoder walks a yaml.Node tree, recording diagnostics instead of stopping
// at the first problem, so one run of validate reports everything.
type decoder struct {
	diags *Diagnostics
}

func pos(n *yaml.Node) Pos {
	if n == nil {
		return Pos{}
	}
	return Pos{Line: n.Line, Col: n.Column}
}

func isNull(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

func kindName(n *yaml.Node) string {
	switch {
	case n == nil:
		return "nothing"
	case isNull(n):
		return "null"
	case n.Kind == yaml.MappingNode:
		return "a mapping"
	case n.Kind == yaml.SequenceNode:
		return "a list"
	case n.Kind == yaml.ScalarNode:
		return "a single value"
	}
	return "an unsupported YAML construct"
}

// mapping reads one YAML mapping, tracking which keys were used so unknown
// keys can be reported when done is called.
type mapping struct {
	d     *decoder
	node  *yaml.Node
	path  string
	keys  []string
	vals  map[string]*yaml.Node
	knpos map[string]Pos
	used  map[string]bool
	known []string
}

// mapping returns a reader for n. If n is not a mapping it reports an error
// and returns a reader with no keys, so callers need no special case.
func (d *decoder) mapping(n *yaml.Node, path string) *mapping {
	m := &mapping{d: d, node: n, path: path, vals: map[string]*yaml.Node{}, knpos: map[string]Pos{}, used: map[string]bool{}}
	if n == nil {
		return m
	}
	if n.Kind != yaml.MappingNode {
		d.diags.errorf(pos(n), path, "expected a mapping (key: value lines), found %s", kindName(n))
		return m
	}
	for _, kv := range d.entries(n, path) {
		k, v := kv[0], kv[1]
		m.keys = append(m.keys, k.Value)
		m.vals[k.Value] = v
		m.knpos[k.Value] = pos(k)
	}
	return m
}

func (m *mapping) sub(key string) string {
	if m.path == "" {
		return key
	}
	return m.path + "." + key
}

// get returns the value node for key (nil if absent) and marks it known.
func (m *mapping) get(key string) *yaml.Node {
	m.known = append(m.known, key)
	m.used[key] = true
	return m.vals[key]
}

func (m *mapping) has(key string) bool {
	_, ok := m.vals[key]
	return ok
}

// done reports keys that no reader asked for.
func (m *mapping) done() {
	for _, k := range m.keys {
		if m.used[k] {
			continue
		}
		msg := fmt.Sprintf("unknown key %q", k)
		if s := suggest(k, m.known); s != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", s)
		}
		m.d.diags.errorf(m.knpos[k], m.path, "%s", msg)
	}
}

// scalar checks n is a non-null scalar and returns its text.
func (d *decoder) scalar(n *yaml.Node, path string) (string, bool) {
	if n == nil {
		return "", false
	}
	if n.Kind != yaml.ScalarNode || isNull(n) {
		d.diags.errorf(pos(n), path, "expected a single value, found %s", kindName(n))
		return "", false
	}
	return n.Value, true
}

// str reads a string. Required strings that are absent or empty are errors.
func (m *mapping) str(key string, required bool) string {
	n := m.get(key)
	if n == nil {
		if required {
			m.d.diags.errorf(pos(m.node), m.path, "missing required key %q", key)
		}
		return ""
	}
	v, ok := m.d.scalar(n, m.sub(key))
	if ok && required && strings.TrimSpace(v) == "" {
		m.d.diags.errorf(pos(n), m.sub(key), "must not be empty")
	}
	return v
}

// optStr reads an optional, nullable string.
func (m *mapping) optStr(key string) Opt[string] {
	n := m.get(key)
	if n == nil {
		return Opt[string]{}
	}
	if isNull(n) {
		return Opt[string]{Set: true, Null: true}
	}
	v, _ := m.d.scalar(n, m.sub(key))
	return Some(v)
}

// boolean reads a boolean, false if absent.
func (m *mapping) boolean(key string) bool {
	return m.optBool(key).V
}

// optBool reads an optional boolean. Only true and false are accepted, not
// YAML 1.1 spellings such as yes/no.
func (m *mapping) optBool(key string) Opt[bool] {
	n := m.get(key)
	if n == nil {
		return Opt[bool]{}
	}
	if isNull(n) {
		return Opt[bool]{Set: true, Null: true}
	}
	b, ok := m.d.boolValue(n, m.sub(key))
	if !ok {
		return Opt[bool]{}
	}
	return Some(b)
}

func (d *decoder) boolValue(n *yaml.Node, path string) (bool, bool) {
	v, ok := d.scalar(n, path)
	if !ok {
		return false, false
	}
	switch {
	case n.Tag == "!!bool" && v == "true":
		return true, true
	case n.Tag == "!!bool" && v == "false":
		return false, true
	}
	d.diags.errorf(pos(n), path, "expected true or false, found %q", v)
	return false, false
}

// number reads a float.
func (m *mapping) number(key string) float64 {
	n := m.get(key)
	v, ok := m.d.scalar(n, m.sub(key))
	if !ok {
		if n == nil {
			m.d.diags.errorf(pos(m.node), m.path, "missing required key %q", key)
		}
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		m.d.diags.errorf(pos(n), m.sub(key), "expected a number, found %q", v)
	}
	return f
}

// strList reads a list of strings. A single value is not accepted in place
// of a list, so that the canonical form is unambiguous.
func (m *mapping) strList(key string) []string {
	return m.d.strList(m.get(key), m.sub(key))
}

func (d *decoder) strList(n *yaml.Node, path string) []string {
	if n == nil {
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		d.diags.errorf(pos(n), path, "expected a list, found %s", kindName(n))
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for i, item := range n.Content {
		if v, ok := d.scalar(item, fmt.Sprintf("%s[%d]", path, i)); ok {
			out = append(out, v)
		}
	}
	return out
}

// list returns the items of a sequence, or nil after an error.
func (m *mapping) list(key string) []*yaml.Node {
	n := m.get(key)
	if n == nil || isNull(n) {
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		m.d.diags.errorf(pos(n), m.sub(key), "expected a list, found %s", kindName(n))
		return nil
	}
	return n.Content
}

// pairs returns the key/value nodes of a mapping-valued key, in file order.
func (m *mapping) pairs(key string) (keys, vals []*yaml.Node) {
	n := m.get(key)
	if n == nil || isNull(n) {
		return nil, nil
	}
	if n.Kind != yaml.MappingNode {
		m.d.diags.errorf(pos(n), m.sub(key), "expected a mapping, found %s", kindName(n))
		return nil, nil
	}
	for _, kv := range m.d.entries(n, m.sub(key)) {
		keys = append(keys, kv[0])
		vals = append(vals, kv[1])
	}
	return keys, vals
}

// strMap reads a mapping of string to string, in file order.
func (m *mapping) strMap(key string) []KV {
	keys, vals := m.pairs(key)
	out := make([]KV, 0, len(keys))
	for i, k := range keys {
		if v, ok := m.d.scalar(vals[i], m.sub(key)+"."+k.Value); ok {
			out = append(out, KV{Key: k.Value, Value: v})
		}
	}
	return out
}

// mode reads an object's mode, applying a default when absent.
func (m *mapping) mode(def Mode, allowed ...Mode) Mode {
	v := m.str("mode", def == "")
	if v == "" {
		return def
	}
	for _, a := range allowed {
		if Mode(v) == a {
			return a
		}
	}
	names := make([]string, len(allowed))
	for i, a := range allowed {
		names[i] = string(a)
	}
	m.d.diags.errorf(pos(m.vals["mode"]), m.sub("mode"), "must be one of %s, found %q", strings.Join(names, ", "), v)
	return def
}

// meta reads the keys every named object may carry.
func (m *mapping) meta() Meta {
	return Meta{
		Pos:    pos(m.node),
		ID:     m.str("id", false),
		Manual: m.boolean("manual"),
		Notes:  m.strList("notes"),
	}
}

// anyValue converts a node into plain Go values (for raw sections and
// properties).
func (d *decoder) anyValue(n *yaml.Node, path string) any {
	switch n.Kind {
	case yaml.ScalarNode:
		return ScalarValue{Tag: n.Tag, Value: n.Value}
	case yaml.SequenceNode:
		out := make([]any, len(n.Content))
		for i, c := range n.Content {
			out[i] = d.anyValue(c, fmt.Sprintf("%s[%d]", path, i))
		}
		return out
	case yaml.MappingNode:
		out := make([]KVAny, 0, len(n.Content)/2)
		for _, kv := range d.entries(n, path) {
			out = append(out, KVAny{Key: kv[0].Value, Value: d.anyValue(kv[1], path+"."+kv[0].Value)})
		}
		return out
	}
	// checkcover:unreachable Parse unwraps the document node and stops at any
	// alias before decoding, so no other kind reaches here.
	panic(fmt.Sprintf("spec: anyValue at %s: unexpected YAML node kind %v", path, n.Kind))
}

// ScalarValue is a raw scalar kept exactly as written (YAML tag and text),
// so that values such as dates or version numbers are not reinterpreted.
type ScalarValue struct {
	Tag, Value string
}

// KVAny is an ordered mapping entry with an arbitrary value; raw mappings
// keep their key order so they round-trip exactly.
type KVAny struct {
	Key   string
	Value any
}

// entries returns a mapping node's key/value pairs, refusing repeated keys.
// The YAML library's node API keeps repeated keys (only its struct decoding
// rejects them), so this is the one place every mapping in the format is
// checked for them.
func (d *decoder) entries(n *yaml.Node, path string) [][2]*yaml.Node {
	seen := map[string]Pos{}
	var out [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if first, dup := seen[k.Value]; dup {
			d.diags.errorf(pos(k), path, "key %q appears more than once (first on line %d)", k.Value, first.Line)
			continue
		}
		seen[k.Value] = pos(k)
		out = append(out, [2]*yaml.Node{k, v})
	}
	return out
}
