package spec

import "testing"

func TestText(t *testing.T) {
	tr := Transition{Pos: Pos{Line: 9}, ID: "11", Name: "Start", From: []string{"Open"}, To: "In Progress",
		Properties: []Property{{Key: "b", Value: "2"}, {Key: "a", Value: "1"}}}
	if got := Text(tr); got != "{name: Start, from: [Open], to: In Progress, properties: {a: \"1\", b: \"2\"}}" {
		t.Errorf("Text(transition) = %s", got)
	}
	if got := Text(WorkflowStatus{Name: "Open", Deprecated: true}); got != "{name: Open, deprecated: true}" {
		t.Errorf("Text(status) = %s", got)
	}
	if got := Text(WorkflowStatus{Name: "Open"}); got != "Open" {
		t.Errorf("Text(plain status) = %s", got)
	}
	if got := Text(Option{Value: "Host", Children: []Option{{Value: "Linux"}}}); got != "{value: Host, children: [Linux]}" {
		t.Errorf("Text(option) = %s", got)
	}
}

func TestTextOfSpaceParts(t *testing.T) {
	c := FieldContext{IssueTypes: []string{"Bug"}, Options: []Option{{Value: "x"}}, Binding: &ContainerRef{Name: "C"}}
	if got := Text(c); got != "{issue_types: [Bug], options: [x]}" {
		t.Errorf("Text(context) = %s", got)
	}
	f := SpaceField{Name: "Summary", FieldSettings: FieldSettings{Required: Some(true)}}
	if got := Text(f); got != "{name: Summary, required: true}" {
		t.Errorf("Text(field) = %s", got)
	}
}
