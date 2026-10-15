package reader

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// TestDefaultValue covers the conversions and notes of context defaults that
// the recorded spaces do not reach.
func TestDefaultValue(t *testing.T) {
	anyType := func(v string) []issueTypeDefault {
		return []issueTypeDefault{{IsAnyIssueType: true, Value: json.RawMessage(v)}}
	}
	perType := func(vs ...string) []issueTypeDefault {
		var out []issueTypeDefault
		for i := 0; i+1 < len(vs); i += 2 {
			out = append(out, issueTypeDefault{IssueTypeID: vs[i], Value: json.RawMessage(vs[i+1])})
		}
		return out
	}
	options := []contextOption{{ID: "1", Value: "One"}, {ID: "2", Value: "Two", OptionID: "1"}}
	tests := []struct {
		name     string
		entries  []issueTypeDefault
		types    []string
		want     string // "" for no default
		wantNote string // "" for no note
	}{
		{"single option", anyType(`{"type":"option.single","optionId":"1"}`), nil, "One", ""},
		{"unknown option kept by ID", anyType(`{"type":"option.single","optionId":"9"}`), nil, "id:9", "not among its options"},
		{"empty multi option is no default", anyType(`{"type":"option.multiple","optionIds":[]}`), nil, "", ""},
		{"cascading parent only", anyType(`{"type":"option.cascading","optionId":"1"}`), nil, "[One]", ""},
		{"same value per issue type folds", perType("10", `{"type":"textfield","text":"x"}`, "11", `{"type":"textfield","text":"x"}`), []string{"10", "11"}, "x", ""},
		{"different values per issue type", perType("10", `{"type":"textfield","text":"x"}`, "11", `{"type":"textfield","text":"y"}`), []string{"10", "11"}, "", "different default values per issue type"},
		{"a covered issue type without a value", perType("11", `{"type":"textfield","text":"x"}`), []string{"10", "11"}, "", "different default values per issue type"},
		{"a value for only some covered issue types", perType("10", `{"type":"textfield","text":"x"}`), []string{"10", "11"}, "", "different default values per issue type"},
		{"unreadable value", anyType(`{"type":"float","number":"many"}`), nil, "", "could not read"},
		{"unknown type", anyType(`{"type":"forge.object","value":{}}`), nil, "", `of type "forge.object", which the format has no form for`},
		{"date time", anyType(`{"type":"datetimepicker","dateTime":"2026-10-14T09:00:00.000+0000"}`), nil, "2026-10-14T09:00:00.000+0000", ""},
		{"date without a value", anyType(`{"type":"datepicker"}`), nil, "", "the format has no form for; set it in Jira by hand"},
		{"user filter", anyType(`{"type":"single.user.select","accountId":"a","userFilter":{"enabled":true}}`), nil, "user:a", "user filter"},
		{"multi user", anyType(`{"type":"multi.user.select","accountIds":["a","b"]}`), nil, "[user:a user:b]", ""},
		{"no multi user is no default", anyType(`{"type":"multi.user.select","accountIds":[]}`), nil, "", ""},
		{"labels", anyType(`{"type":"labels","labels":["a"]}`), nil, "[a]", ""},
		{"null value is no default", anyType(`null`), nil, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &Reader{}
			got, ok := e.defaultValue(spec.CustomField{GlobalObject: spec.GlobalObject{Name: "F"}}, tc.entries, tc.types, options)
			var s string
			if ok {
				if got.V.IsList {
					s = "[" + strings.Join(got.V.List, " ") + "]"
				} else {
					s = got.V.Str
				}
			}
			if s != tc.want {
				t.Errorf("default = %q (ok %v), want %q", s, ok, tc.want)
			}
			notes := strings.Join(e.notes, "\n")
			switch {
			case tc.wantNote == "" && notes != "":
				t.Errorf("unexpected note: %s", notes)
			case tc.wantNote != "" && !strings.Contains(notes, tc.wantNote):
				t.Errorf("notes %q do not contain %q", notes, tc.wantNote)
			}
		})
	}
}
