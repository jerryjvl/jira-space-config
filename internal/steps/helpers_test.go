package steps

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// pages is a Sender with a list of n numbers, served 100 a page as Jira does.
type pages struct {
	n       int
	queries []url.Values
}

func (p *pages) Send(_ context.Context, _, _ string, q url.Values, _, out any) error {
	p.queries = append(p.queries, q)
	start, _ := strconv.Atoi(q.Get("startAt"))
	end := min(start+100, p.n)
	values := ""
	for i := start; i < end; i++ {
		if values != "" {
			values += ","
		}
		values += strconv.Itoa(i)
	}
	return decode(fmt.Sprintf(`{"isLast":%v,"values":[%s]}`, end >= p.n, values), out)
}

func TestPagedGetReadsEveryPage(t *testing.T) {
	for _, n := range []int{0, 1, 100, 101, 250} {
		p := &pages{n: n}
		got, err := pagedGet[int](context.Background(), p, "/rest/x", url.Values{"schemeId": {"7"}})
		if err != nil || len(got) != n {
			t.Fatalf("%d values: got %d, %v", n, len(got), err)
		}
		for i, v := range got {
			if v != i {
				t.Fatalf("%d values: value %d is %d", n, i, v)
			}
		}
		for _, q := range p.queries {
			if q.Get("schemeId") != "7" || q.Get("maxResults") != "100" {
				t.Errorf("%d values: query %v", n, q)
			}
		}
	}
	if _, err := pagedGet[int](context.Background(), replies(func(string, string, any) error { return fmt.Errorf("boom") }), "/rest/x", nil); err == nil {
		t.Error("a failed read must fail")
	}
}

func TestAddedAndRemoved(t *testing.T) {
	for _, tc := range []struct{ want, got, add, remove string }{
		{"a,b", "a,b", "", ""},
		{"a,b,c", "b", "a,c", ""},
		{"b", "a,b,c", "", "a,c"},
		{"x", "y", "x", "y"},
		{"", "", "", ""},
	} {
		split := func(s string) []string {
			if s == "" {
				return nil
			}
			return strings.Split(s, ",")
		}
		add, remove := addedAndRemoved(split(tc.want), split(tc.got))
		if strings.Join(add, ",") != tc.add || strings.Join(remove, ",") != tc.remove {
			t.Errorf("%q vs %q: add %v, remove %v", tc.want, tc.got, add, remove)
		}
	}
}

// TestGroupDescriptionIsNotCompared: Jira keeps no description for a group,
// so one in the spec must not make a run after the group's creation fail.
func TestGroupDescriptionIsNotCompared(t *testing.T) {
	_, env := testSite(t)
	env.Spec.Groups = []spec.GlobalObject{{Name: "team-alpha", Mode: spec.ModeEnsure, Description: spec.Some("Alpha team")}}
	st, err := groups(env)
	if err != nil || len(st.Items) != 1 || st.Items[0].Kind != Exists {
		t.Errorf("%+v, %v", st, err)
	}
}
