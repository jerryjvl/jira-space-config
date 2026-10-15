package apply

import (
	"context"
	"fmt"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/steps"
	"github.com/jerryjvl/jira-space-config/internal/util"
	"github.com/jerryjvl/jira-space-config/internal/whoami"
)

// session is what a run knows after preflight.
type session struct {
	spec    *spec.Spec
	report  whoami.Report
	project *reader.Project
	env     *steps.Env
}

// prepare checks the spec, runs preflight and prints the header: everything
// a dry run and a live run share before the plan. A problem is printed and
// returned as a *Stopped error.
func prepare(ctx context.Context, c *jira.Client, opts Options, p *printer) (*session, error) {
	// The spec first: no request is made for a spec that cannot be applied.
	s, diags := spec.Parse(opts.Spec)
	if !diags.HasErrors() {
		diags = append(diags, spec.Validate(s)...)
	}
	if n := diags.Count(spec.SeverityError); n > 0 {
		pr := Problem{Title: fmt.Sprintf("The spec has %s", util.Plural(n, "error", "errors")),
			Fix: fmt.Sprintf("Correct them, then run again. 'validate %s' lists them too.", opts.SpecFile)}
		for _, d := range diags.Sorted() {
			if d.Severity == spec.SeverityError {
				pr.Lines = append(pr.Lines, d.Format(opts.SpecFile))
			}
		}
		return nil, stop(p, 1, pr, fmt.Sprintf("%s has %s", opts.SpecFile, util.Plural(n, "error", "errors")))
	}
	if s.Space.Key != opts.SpaceKey {
		return nil, stop(p, 1, Problem{
			Title: fmt.Sprintf("The spec is for space %s, not %s", s.Space.Key, opts.SpaceKey),
			Why:   "--space must name the space the spec describes, so a spec is never applied to another space by mistake.",
			Fix:   fmt.Sprintf("Check --space and the spec file. To use this spec for %s, change space.key in a copy of it.", opts.SpaceKey),
		}, "the spec is for another space")
	}

	// Preflight: who is signed in, with what permissions, and the space.
	report, err := whoami.Run(ctx, c, opts.SpaceKey)
	if err != nil {
		return nil, stop(p, 1, readProblem("Could not check the account and the space", err), "preflight failed")
	}
	if report.Space != nil && report.Space.Style != "classic" {
		return nil, stop(p, 1, Problem{
			Title: fmt.Sprintf("Space %s is team-managed", report.Space.Key),
			Why:   "This tool configures company-managed spaces only; a team-managed space keeps its configuration inside the space, where the site-wide objects and schemes the spec describes do not apply.",
			Fix:   "Use a company-managed space. Jira cannot convert a space from one kind to the other.",
		}, "the space is team-managed")
	}
	r := reader.New(ctx, c)
	project, err := r.Space(opts.SpaceKey)
	if err != nil {
		return nil, stop(p, 1, readProblem("Could not read the space", err), "preflight failed")
	}
	if b := s.Binding; b != nil && b.CloudID != "" && b.CloudID != report.CloudID && !opts.IgnoreIDs {
		return nil, stop(p, 1, Problem{
			Title: "The spec was written for another site",
			Lines: []string{
				fmt.Sprintf("Spec       %s (cloud ID %s)", b.Site, b.CloudID),
				fmt.Sprintf("This site  %s (cloud ID %s)", report.Site, report.CloudID),
			},
			Why: "The spec's IDs belong to the other site; here they would name other objects, or none.",
			Fix: "If this is the site you mean, run with --ignore-ids: objects are then found by name only.",
		}, "the spec is bound to another site")
	}
	items, err := r.WorkItemCount(project)
	if err != nil {
		return nil, stop(p, 1, readProblem("Could not count the space's work items", err), "preflight failed")
	}

	header(p, opts, report, project, items, diags)

	free := false
	for _, plan := range report.Plans {
		free = free || plan.Plan == "FREE"
	}
	browse := false
	for _, perm := range report.Permissions {
		browse = browse || perm.Key == "BROWSE_PROJECTS" && perm.Have
	}
	env := &steps.Env{R: r, Spec: s, P: project, Site: c.Site().String(), IgnoreIDs: opts.IgnoreIDs, AllowUpdate: opts.AllowUpdate, AllowDelete: opts.AllowDelete, CheckUsage: opts.CheckUsage, AdministerJira: report.AdministerJira,
		FreePlan: free, WorkItems: items, Browse: browse}
	return &session{spec: s, report: report, project: project, env: env}, nil
}
