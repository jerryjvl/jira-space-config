package spec

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Part is a part of an object that Text renders.
type Part interface {
	Transition | WorkflowStatus | Option | Rule | FieldContext | SpaceField
}

// Text renders one part of a spec as canonical YAML on one line, for
// comparing parts of objects and showing how they differ. Positions never
// matter; a transition's ID (a cross-check, not part of what it does) and a
// field context's binding are left out.
func Text[T Part](v T) string {
	w := &writer{block: map[*yaml.Node]bool{}}
	var n *yaml.Node
	switch x := any(v).(type) {
	case Transition:
		x.ID = ""
		n = w.transition(x)
	case WorkflowStatus:
		n = w.workflow(Workflow{Statuses: []WorkflowStatus{x}}).Content[1].Content[0] // statuses: [x]
	case Option:
		n = optionNode(x)
	case Rule:
		n = ruleNode(x)
	case FieldContext:
		x.Binding = nil // which context implements it is compared separately
		n = w.fieldContext(x)
	case SpaceField:
		n = spaceFieldNode(x)
	default:
		// checkcover:unreachable Part admits only the types above.
		panic(fmt.Sprintf("spec.Text: unsupported %T", v))
	}
	flow(n)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(n); err != nil {
		// checkcover:unreachable the writer builds only nodes that encode.
		panic(fmt.Sprintf("spec.Text: %v", err))
	}
	_ = enc.Close()
	// Long flow values may be folded over lines; a fold reads as a space.
	return strings.Join(strings.Fields(buf.String()), " ")
}

func flow(n *yaml.Node) {
	if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode {
		n.Style = yaml.FlowStyle
	}
	for _, c := range n.Content {
		flow(c)
	}
}
