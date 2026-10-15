// Package apply runs a spec against a site: the dry run (every step
// evaluated and the whole plan printed, through a client that cannot write)
// and the live run (live.go). What the operator sees is in docs/cmdline.md.
package apply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/steps"
	"github.com/jerryjvl/jira-space-config/internal/util"
	"github.com/jerryjvl/jira-space-config/internal/whoami"
)

// Options configure a run.
type Options struct {
	SpecFile string // as the operator named it, for messages
	Spec     []byte
	SpaceKey string
	// IgnoreIDs plans by name only, for a spec written for another site.
	IgnoreIDs    bool
	ShowRequests bool
	Colour       bool
	// AllowUpdate are the kinds of change the operator allowed (--allow-update).
	AllowUpdate []string
	// AllowDelete are the kinds of removal the operator allowed (--allow-delete).
	AllowDelete []string
	// CheckUsage are the kinds whose usage may be checked space by space (--check-usage).
	CheckUsage []string
}

// Stopped is returned when the run stops at a problem; the problem has
// already been printed.
type Stopped struct{ Summary string }

func (s *Stopped) Error() string { return s.Summary }

// DryRun evaluates every step and prints the plan to w. It returns a
// *Stopped error when the spec cannot be applied as it is: a problem stopped
// the run, or items need fixing first.
func DryRun(ctx context.Context, c *jira.Client, opts Options, w io.Writer) error {
	p := &printer{w: w, colour: opts.Colour, showRequests: opts.ShowRequests}
	err := dryRun(ctx, c, opts, p)
	if p.err != nil {
		return p.err
	}
	return err
}

func dryRun(ctx context.Context, c *jira.Client, opts Options, p *printer) error {
	p.line("jira-space-config apply · DRY RUN · nothing will be changed")
	p.blank()

	sess, err := prepare(ctx, c, opts, p)
	if err != nil {
		return err
	}
	s, env := sess.spec, sess.env
	var evaluated []*steps.Step
	for _, def := range steps.All {
		if !def.Has(s) {
			continue
		}
		st, err := def.Eval(env)
		if err != nil {
			pr := readProblem(fmt.Sprintf("Could not evaluate step %d of %d · %s", def.Number, steps.Total, def.Title), err)
			return stop(p, def.Number, pr, fmt.Sprintf("stopped at step %d", def.Number))
		}
		evaluated = append(evaluated, st)
	}

	overview(p, s, evaluated)
	for _, st := range evaluated {
		p.step(st, opts.ShowRequests)
	}
	return summary(p, evaluated, c.Requests())
}

func header(p *printer, opts Options, report whoami.Report, project *reader.Project, items int, diags spec.Diagnostics) {
	tick := func(have bool) string {
		if have {
			return "✓"
		}
		return "✗"
	}
	have := map[string]bool{}
	for _, perm := range report.Permissions {
		have[perm.Key] = perm.Have
	}
	adminProjects := tick(have["ADMINISTER_PROJECTS"])
	if !have["ADMINISTER_PROJECTS"] && report.AdministerJira {
		adminProjects = "✓ (through Administer Jira)"
	}
	workItems := util.Plural(items, "work item", "work items")
	if !have["BROWSE_PROJECTS"] {
		workItems += " that this account can see (it cannot browse the space, so there may be more)"
	}
	sum := sha256.Sum256(opts.Spec)
	var given []string
	if opts.IgnoreIDs {
		given = append(given, "--ignore-ids (objects are found by name only)")
	}
	for _, a := range opts.AllowUpdate {
		given = append(given, "--allow-update="+a)
	}
	for _, a := range opts.AllowDelete {
		given = append(given, "--allow-delete="+a)
	}
	for _, a := range opts.CheckUsage {
		given = append(given, "--check-usage="+a)
	}
	overrides := "none"
	if len(given) > 0 {
		overrides = strings.Join(given, " · ")
	}
	rows := []struct{ label, text string }{
		{"Site", report.Site},
		{"Space", fmt.Sprintf("%s %q · company-managed · %s", project.Key, project.Details.Name.V, workItems)},
		{"Account", fmt.Sprintf("%s · Administer Jira %s · Administer projects %s",
			report.User.DisplayName, tick(report.AdministerJira), adminProjects)},
		{"Spec", specLine(opts.SpecFile, diags)},
		{"Spec hash", "sha256:" + hex.EncodeToString(sum[:])},
		{"Overrides", overrides},
	}
	for _, row := range rows {
		p.labelled(2, 11, row.label, row.text)
	}
	for _, n := range report.Notes() {
		p.labelled(2, 11, "Note", n)
	}
	p.blank()
}

func overview(p *printer, s *spec.Spec, evaluated []*steps.Step) {
	changes, attention := 0, 0
	for _, st := range evaluated {
		if st.Count(steps.Create)+st.Count(steps.Update) > 0 {
			changes++
		}
		if st.Blocking()+st.Count(steps.Manual) > 0 {
			attention++
		}
	}
	p.line("Plan · %s evaluated · %s · %s", util.Plural(len(evaluated)+1, "step", "steps"),
		util.Plural(changes, "with changes", "with changes"), util.Plural(attention, "needs attention", "need attention"))
	p.blank()
	if s.Binding == nil || s.Binding.Site == "" {
		p.para(2, "First apply: the spec has no binding, so it has not been applied or exported before, "+
			"and creations are expected.")
		p.blank()
	}
	row := func(number, title, text string) {
		p.line("  %s%s  %-32s %s", strings.Repeat(" ", max(0, 5-utf8.RuneCountInString(number))), number, title, text)
	}
	row("1", "Preflight", "ok")
	for _, st := range evaluated {
		row(strconv.Itoa(st.Number), st.Title, counts(st))
	}
	p.blank()
}

func summary(p *printer, evaluated []*steps.Step, requests int) error {
	var nouns []string // in step order
	createdBy := map[string]int{}
	plurals := map[string]string{}
	creates, siteWide, matched, manual, changes := 0, 0, 0, 0, 0
	var fixing []string
	blocking := 0
	for _, st := range evaluated {
		for _, it := range st.Items {
			if it.Kind == steps.Create && it.SiteWide {
				siteWide++
			}
			if it.Kind == steps.Update && it.Later == "" {
				changes++
			}
		}
		if n := st.Count(steps.Create); n > 0 {
			creates += n
			if createdBy[st.Noun] == 0 {
				nouns = append(nouns, st.Noun)
			}
			createdBy[st.Noun] += n
			plurals[st.Noun] = st.Plural
		}
		matched += st.Count(steps.Exists)
		manual += st.Count(steps.Manual)
		if n := st.Blocking(); n > 0 {
			blocking += n
			fixing = append(fixing, strconv.Itoa(st.Number))
		}
	}
	p.line("Summary · dry run")
	row := func(label string, n int, detail string) {
		text := strconv.Itoa(n)
		if detail != "" {
			text += " · " + detail
		}
		p.labelled(2, 15, label, text)
	}
	var created []string
	for _, noun := range nouns {
		created = append(created, util.Plural(createdBy[noun], noun, plurals[noun]))
	}
	detail := strings.Join(created, ", ")
	if siteWide > 0 {
		detail += fmt.Sprintf(" (%d site-wide, shared by every space)", siteWide)
	}
	row("Would create", creates, detail)
	if changes > 0 {
		row("Would change", changes, "")
	}
	row("Matched", matched, "")
	if blocking > 0 {
		row("Needs fixing", blocking, "steps "+list(fixing))
	} else {
		row("Needs fixing", 0, "")
	}
	if manual > 0 {
		row("Manual", manual, "compared only; yours to resolve")
	}
	p.blank()
	p.line("Nothing was changed. %s made, all read-only.", util.Plural(requests, "request", "requests"))
	if blocking > 0 {
		return &Stopped{Summary: fmt.Sprintf("%s to fix before the spec can be applied",
			util.Plural(blocking, "item", "items"))}
	}
	return nil
}

// stop prints a problem and how far the run got.
func stop(p *printer, step int, pr Problem, summary string) error {
	p.problem(pr)
	p.line("Stopped at step %d of %d. Nothing was changed.", step, steps.Total)
	return &Stopped{Summary: summary}
}

// readProblem describes a failed read: what Jira answered, what the tool
// was doing, and the fix the client knows.
func readProblem(title string, err error) Problem {
	pr := Problem{Title: title, Fix: "Run again once the cause is fixed; nothing was changed, so that is always safe."}
	var apiErr *jira.APIError
	var transportErr *jira.TransportError
	switch {
	case errors.As(err, &apiErr):
		pr.Request = apiErr.Method + " " + apiErr.Path
		pr.JiraAnswered = strings.TrimPrefix(apiErr.Error(), apiErr.Method+" "+apiErr.Path+": ")
		pr.While = strings.TrimSuffix(strings.TrimSuffix(err.Error(), apiErr.Error()), ": ")
		if pr.While == "" {
			pr.While = "reading " + apiErr.Method + " " + apiErr.Path
		}
		if h := apiErr.Hint(); h != "" {
			pr.Fix = h
		}
	case errors.As(err, &transportErr):
		pr.While = err.Error()
		pr.Fix = transportErr.Hint()
	default:
		pr.While = err.Error()
	}
	return pr
}

// list joins items as "3, 4 and 8".
func list(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// specLine says the spec is valid and how many warnings it has; the
// warnings themselves belong to the steps they concern, and validate lists them.
func specLine(file string, diags spec.Diagnostics) string {
	line := file + " · valid"
	if n := diags.Count(spec.SeverityWarning); n > 0 {
		them := "them"
		if n == 1 {
			them = "it"
		}
		line += fmt.Sprintf(" · %s ('validate %s' lists %s)", util.Plural(n, "warning", "warnings"), file, them)
	}
	return line
}
