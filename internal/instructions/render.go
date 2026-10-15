package instructions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// holderLabels are the holder kinds in words; one with a value is followed
// by it.
var holderLabels = map[string]string{
	"role": "Role", "group": "Group", "user": "User", "user_field": "User in field", "group_field": "Group in field",
	"application_role": "Application access", "email": "Email", "reporter": "Reporter", "current_assignee": "Current assignee",
	"space_lead": "Space lead", "anyone": "Anyone", "current_user": "Current user", "component_lead": "Component lead",
	"all_watchers": "All watchers",
}

func (d *doc) holder(h string) string {
	if h == "" {
		return ""
	}
	kind, v, _ := strings.Cut(h, ":")
	label, ok := holderLabels[kind]
	switch {
	case !ok:
		return h
	case kind == "application_role" && v == "":
		return "Any application access"
	case v == "":
		return label
	case kind == "role":
		// The spec gives a role's ID when the site has others of its name.
		for _, r := range d.s.Roles {
			if r.Name == v && r.ID != "" {
				return label + " " + v + " (id " + r.ID + ")"
			}
		}
	}
	return label + " " + v
}

func (d *doc) holders(hs []string) string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = d.holder(h)
	}
	return strings.Join(out, ", ")
}

// componentLabels name an automation component's kind as the rule editor
// shows it.
var componentLabels = map[string]string{"TRIGGER": "When", "CONDITION": "If", "ACTION": "Then", "BRANCH": "For",
	"CONDITION_BLOCK": "Block", "ORCHESTRATION": "Run"}

// ruleOutline is an automation rule's trigger and components as a nested
// list of their kinds and types.
func ruleOutline(body map[string]any) string {
	var b strings.Builder
	if t, ok := body["trigger"].(map[string]any); ok {
		component(&b, t, 0)
	}
	if cs, ok := body["components"].([]any); ok {
		for _, c := range cs {
			if m, ok := c.(map[string]any); ok {
				component(&b, m, 0)
			}
		}
	}
	return b.String()
}

func component(b *strings.Builder, c map[string]any, depth int) {
	kind, _ := c["component"].(string)
	label, ok := componentLabels[kind]
	if !ok {
		label = kind
	}
	typ, _ := c["type"].(string)
	fmt.Fprintf(b, "%s- **%s:** %s\n", strings.Repeat("  ", depth), label, code(typ))
	for _, key := range []string{"conditions", "children"} {
		list, _ := c[key].([]any)
		for _, sub := range list {
			if m, ok := sub.(map[string]any); ok {
				component(b, m, depth+1)
			}
		}
	}
}

func jsonText(v any) string {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v) // JSONValue gives only values that marshal
	}
	return string(out)
}

// yamlText writes a raw value as YAML, its keys in the order written.
func yamlText(v any) string {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(rawNode(v)); err != nil {
		return fmt.Sprint(v) // rawNode builds only nodes that encode
	}
	_ = enc.Close()
	return buf.String()
}

// rawStrings is a raw scalar or list of scalars as strings.
func rawStrings(v any) []string {
	switch x := v.(type) {
	case spec.ScalarValue:
		return []string{x.Value}
	case []any:
		var out []string
		for _, item := range x {
			out = append(out, rawStrings(item)...)
		}
		return out
	}
	return []string{strings.TrimSpace(yamlText(v))}
}

func rawNode(v any) *yaml.Node {
	switch x := v.(type) {
	case spec.ScalarValue:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: x.Tag, Value: x.Value}
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range x {
			n.Content = append(n.Content, rawNode(item))
		}
		return n
	case []spec.KVAny:
		n := &yaml.Node{Kind: yaml.MappingNode}
		for _, kv := range x {
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: kv.Key}, rawNode(kv.Value))
		}
		return n
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
}
