package export

import "testing"

// TestManagedPrefix checks the prefix is read back from the managed names.
func TestManagedPrefix(t *testing.T) {
	cases := []struct {
		names  []string
		want   string
		wantOK bool
	}{
		{nil, "", true},
		// A generated scheme name gives the prefix, even when every name shares more words.
		{[]string{"JSC3 Bug Workflow", "JSC3 Bug Issue Type Scheme"}, "JSC3 Bug", true},
		{[]string{"JSC3 Bug Workflow", "JSC3 Field Scheme"}, "JSC3", true},
		{[]string{"Team A Workflow", "Team A Triage Screen", "Team A Workflow Scheme"}, "Team A", true},
		// A scheme name whose leading words other names lack does not count.
		{[]string{"Ops Workflow", "Ops Bug Field Scheme"}, "Ops", true},
		// Without a generated name, the shared leading words.
		{[]string{"VULN Triage Screen", "VULN Workflow"}, "VULN", true},
		{[]string{"VULN Workflow"}, "VULN", true},
		{[]string{"VULN Workflow", "OPS Workflow"}, "", false},
		{[]string{"Workflow"}, "", false},
	}
	for _, c := range cases {
		got, ok := managedPrefix(c.names)
		if got != c.want || ok != c.wantOK {
			t.Errorf("managedPrefix(%q) = %q, %v; want %q, %v", c.names, got, ok, c.want, c.wantOK)
		}
	}
}
