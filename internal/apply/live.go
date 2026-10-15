package apply

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/export"
	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/steps"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// LiveOptions configure a live run, in addition to Options.
type LiveOptions struct {
	Options
	Connect jira.Options // how the run's clients reach the site
	In      io.Reader    // the operator's answers (a terminal)
	// StateDir holds the run's lock and audit log; Output is where the
	// resulting spec is written.
	StateDir, Output string
	ToolVersion      string
}

// Live applies a spec, one confirmed step at a time (docs/cmdline.md, "A
// live run"). Each step is evaluated afresh, shown, and carried out
// only when the operator proceeds; every write goes through a writer that
// accepts only the writes the step planned, and is logged.
func Live(ctx context.Context, opts LiveOptions, w io.Writer) error {
	p := &printer{w: w, colour: opts.Colour, showRequests: opts.ShowRequests}
	err := live(ctx, opts, p)
	if p.err != nil {
		return p.err
	}
	return err
}

// run is the state of one live run.
type run struct {
	opts     LiveOptions
	p        *printer
	in       *bufio.Reader
	sess     *session
	writer   *jira.Writer
	audit    *auditLog
	ctx      context.Context
	answers  chan string
	created  []string // "status \"In Triage\"", in order
	updated  []string
	partly   []string // items that failed after some of their writes went through
	approved bool     // the operator typed the space key
}

func live(ctx context.Context, opts LiveOptions, p *printer) error {
	p.line("jira-space-config apply · LIVE · changes will be made, one confirmed step at a time")
	p.blank()
	c, err := jira.New(ctx, opts.Connect)
	if err != nil {
		return err
	}
	sess, err := prepare(ctx, c, opts.Options, p)
	if err != nil {
		return err
	}
	host := c.Site().Host()
	l, err := lockSpace(opts.StateDir+"/locks", host, opts.SpaceKey)
	var held *lockHeld
	if errors.As(err, &held) {
		return stop(p, 1, Problem{
			Title: fmt.Sprintf("Another live run is applying to %s on %s", opts.SpaceKey, host),
			Lines: []string{"Lock file  " + held.path, "Held by    " + held.by},
			Why:   "Two runs at once could each create what the other is creating.",
			Fix:   "Wait for the other run to finish. If none is running (it was killed), delete the lock file.",
		}, "another live run holds the lock")
	}
	if err != nil {
		return err
	}
	defer l.release()
	audit, err := openAudit(opts.StateDir+"/runs", c.Site().String(), opts.SpaceKey, time.Now())
	if err != nil {
		return err
	}
	defer audit.close()
	writer, err := jira.NewWriter(ctx, opts.Connect, sess.project.Key, sess.project.ID)
	if err != nil {
		return err
	}
	r := &run{opts: opts, p: p, in: bufio.NewReader(opts.In), sess: sess, writer: writer, audit: audit, ctx: ctx}
	r.answers = make(chan string)
	go r.readAnswers()

	err = r.steps()
	r.finish(err)
	return err
}

// readAnswers reads the operator's lines, so a prompt can also end on Ctrl-C.
func (r *run) readAnswers() {
	for {
		line, err := r.in.ReadString('\n')
		if err != nil && line == "" {
			close(r.answers)
			return
		}
		r.answers <- strings.TrimSpace(line)
	}
}

var errAborted = errors.New("aborted")

// ask shows a prompt that names the site and space (protection 9) and
// returns the answer.
func (r *run) ask(prompt string) (string, error) {
	r.p.line("  %s on %s · %s  › ", r.sess.project.Key, r.writer.Site().Host(), prompt)
	select {
	case <-r.ctx.Done():
		return "", errAborted
	case a, ok := <-r.answers:
		if !ok {
			return "", errAborted // end of input
		}
		return a, nil
	}
}

func (r *run) steps() error {
	env := r.sess.env
	for _, def := range steps.All {
		if !def.Has(r.sess.spec) {
			continue
		}
		if err := r.step(env, def); err != nil {
			return err
		}
	}
	return nil
}

// step shows one step and, if it changes something, carries it out once
// the operator proceeds.
func (r *run) step(env *steps.Env, def steps.Definition) error {
	for {
		st, err := r.evaluate(env, def)
		if err != nil {
			return err
		}
		r.p.step(st, r.opts.ShowRequests)
		changes := 0
		for _, it := range st.Items {
			if changesHere(it) {
				changes++
			}
		}
		if changes == 0 && st.Blocking() == 0 {
			return nil
		}
		for _, it := range st.Items {
			if changesHere(it) && it.Do == nil {
				return r.stopHere(st.Number, Problem{
					Title: fmt.Sprintf("This tool cannot carry out step %d (%s)", st.Number, st.Title),
					Why:   fmt.Sprintf("%q needs a change this tool plans but does not make.", it.Name),
					Fix:   "Make the change in Jira by hand and re-run. Nothing in this step was changed.",
				})
			}
		}
		if st.Blocking() > 0 {
			r.p.para(2, fmt.Sprintf("Step %d %s. Fix %s above first; proceed is not available until then.",
				st.Number, planText(st), util.Plural(st.Blocking(), "the item", "the items")))
			a, err := r.ask("[r] re-evaluate   [a] abort")
			if err != nil || a == "a" {
				return r.aborted(st.Number)
			}
			continue // re-evaluate
		}
		r.p.para(2, fmt.Sprintf("Step %d %s.", st.Number, planText(st)))
		a, err := r.ask("[p] proceed   [r] re-evaluate   [a] abort")
		switch {
		case err != nil || a == "a":
			return r.aborted(st.Number)
		case a != "p":
			continue
		}
		if ok, err := r.confirm(st); err != nil || !ok {
			return r.aborted(st.Number)
		}
		// Protection 4: act only on what was shown. Jira is read again; if
		// anything changed, the step is shown again instead.
		again, err := r.evaluate(env, def)
		if err != nil {
			return err
		}
		if signature(again) != signature(st) {
			r.p.para(2, "Jira changed since the step was shown, so it is shown again.")
			r.p.blank()
			continue
		}
		return r.carryOut(again)
	}
}

func (r *run) evaluate(env *steps.Env, def steps.Definition) (*steps.Step, error) {
	env.Refresh(reader.New(r.ctx, r.writer.Client))
	st, err := def.Eval(env)
	if err != nil {
		pr := readProblem(fmt.Sprintf("Could not evaluate step %d of %d · %s", def.Number, steps.Total, def.Title), err)
		return nil, r.stopHere(def.Number, pr)
	}
	return st, nil
}

// signature is what a step would do: compared before and after the
// operator answers.
func signature(st *steps.Step) string {
	var b strings.Builder
	for _, it := range st.Items {
		fmt.Fprintf(&b, "%d %s %s\n", it.Kind, it.Name, it.Summary)
	}
	return b.String()
}

// planText says what a step would do: "will create 2 statuses (site-wide)".
func planText(st *steps.Step) string {
	creates, updates, siteWide := st.Count(steps.Create), 0, 0
	for _, it := range st.Items {
		if it.Kind == steps.Create && it.SiteWide {
			siteWide++
		}
		if it.Kind == steps.Update && changesHere(it) {
			updates++
		}
	}
	var parts []string
	if creates > 0 {
		text := "create " + util.Plural(creates, st.Noun, st.Plural)
		if siteWide > 0 {
			text += ", site-wide: every space on the site shares them"
		}
		parts = append(parts, text)
	}
	if updates > 0 {
		parts = append(parts, "change "+util.Plural(updates, "thing", "things"))
	}
	if len(parts) == 0 {
		return "has nothing to change"
	}
	return "will " + strings.Join(parts, " and ")
}

// confirm asks for the typed confirmations (protections 1 and 7): the space
// key before the run's first write, and the count for unusual volumes.
func (r *run) confirm(st *steps.Step) (bool, error) {
	if !r.approved {
		r.p.para(2, fmt.Sprintf("This is the run's first change to Jira. Type the space key, %s, to go on.", r.sess.project.Key))
		a, err := r.ask("space key")
		if err != nil || a != r.sess.project.Key {
			return false, err
		}
		r.approved = true
	}
	creates, siteWide := st.Count(steps.Create), 0
	for _, it := range st.Items {
		if it.Kind == steps.Create && it.SiteWide {
			siteWide++
		}
	}
	reapply := r.sess.spec.Binding != nil && r.sess.spec.Binding.Site != ""
	var why string
	switch {
	case siteWide > 5:
		why = fmt.Sprintf("This step creates %d site-wide objects, more than a space usually needs.", siteWide)
	case reapply && creates*2 > len(st.Items):
		why = fmt.Sprintf("This spec has been applied before, yet this step creates %d of its %d objects.", creates, len(st.Items))
	default:
		return true, nil
	}
	r.p.para(2, why+fmt.Sprintf(" Type %d to create them.", creates))
	a, err := r.ask("count")
	return err == nil && a == strconv.Itoa(creates), err
}

// carryOut makes a step's changes, one item at a time.
func (r *run) carryOut(st *steps.Step) error {
	defer r.writer.Reset()
	r.p.blank()
	n := 0
	for _, it := range st.Items {
		if !changesHere(it) {
			continue
		}
		if r.ctx.Err() != nil {
			return r.interrupted(st.Number)
		}
		r.writer.Reset()
		for _, req := range it.Requests {
			if method, path, ok := plannedRequest(req); ok {
				r.writer.Allow(method, path)
			}
		}
		object := st.Noun + " " + fmt.Sprintf("%q", it.Name)
		if it.Object != "" {
			object = it.Object
		}
		_ = r.audit.write(map[string]any{"event": "intent", "step": st.Number, "object": object, "requests": it.Requests})
		sender := &auditSender{w: r.writer, audit: r.audit, step: st.Number}
		id, err := it.Do(r.ctx, sender)
		if err != nil {
			if sender.written > 0 {
				r.partly = append(r.partly, fmt.Sprintf("%s (%s made)", object, util.Plural(sender.written, "write", "writes")))
			}
			_ = r.audit.write(map[string]any{"event": "failed", "step": st.Number, "object": object, "error": err.Error()})
			return r.stopHere(st.Number, writeProblem(fmt.Sprintf("Could not %s %s", verb(it.Kind), object), err))
		}
		_ = r.audit.write(map[string]any{"event": "done", "step": st.Number, "object": object, "id": id})
		mark := "+"
		if it.Kind == steps.Update {
			mark = "~"
			r.updated = append(r.updated, object)
		} else {
			r.created = append(r.created, object)
		}
		r.p.line("  %s %s  %sd (id %s)", mark, it.Name, verb(it.Kind), id)
		n++
	}
	r.p.line("  Step %d done · %s", st.Number, util.Plural(n, "change", "changes"))
	r.p.blank()
	return nil
}

func verb(k steps.Kind) string {
	if k == steps.Update {
		return "update"
	}
	return "create"
}

// plannedRequest reads "POST /rest/api/3/screens/{screenId}/tabs × 2 (…)"
// as a method and a path template.
func plannedRequest(req string) (string, string, bool) {
	fields := strings.Fields(req)
	if len(fields) < 2 || !slices.Contains([]string{http.MethodPost, http.MethodPut, http.MethodDelete}, fields[0]) {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// auditSender logs each write before and after it is sent.
type auditSender struct {
	w       *jira.Writer
	audit   *auditLog
	step    int
	written int // writes that went through
}

func (a *auditSender) Send(ctx context.Context, method, apiPath string, query url.Values, body, out any) error {
	_ = a.audit.write(map[string]any{"event": "send", "step": a.step, "method": method, "path": apiPath})
	err := a.w.Send(ctx, method, apiPath, query, body, out)
	result := "ok"
	if err != nil {
		result = err.Error()
	}
	_ = a.audit.write(map[string]any{"event": "sent", "step": a.step, "method": method, "path": apiPath, "result": result})
	if err == nil && jira.IsWrite(method, apiPath) {
		a.written++
	}
	return err
}

func (r *run) aborted(step int) error {
	r.p.blank()
	r.p.line("Aborted at step %d of %d.", step, steps.Total)
	return &Stopped{Summary: fmt.Sprintf("aborted at step %d", step)}
}

func (r *run) interrupted(step int) error {
	r.p.blank()
	r.p.line("Stopped by Ctrl-C at step %d of %d, between two writes.", step, steps.Total)
	return &Stopped{Summary: fmt.Sprintf("stopped at step %d", step)}
}

// stopHere prints a problem with what the run has done so far.
func (r *run) stopHere(step int, pr Problem) error {
	r.p.problem(pr)
	r.p.line("Stopped at step %d of %d.", step, steps.Total)
	return &Stopped{Summary: fmt.Sprintf("stopped at step %d", step)}
}

// finish prints the summary and, after any change, writes the resulting
// spec (an export of the space as it now is).
func (r *run) finish(runErr error) {
	p := r.p
	p.blank()
	p.line("Summary")
	p.labelled(2, 15, "Created", fmt.Sprintf("%d%s", len(r.created), listed(r.created)))
	p.labelled(2, 15, "Updated", fmt.Sprintf("%d%s", len(r.updated), listed(r.updated)))
	if len(r.partly) > 0 {
		p.labelled(2, 15, "Partly changed", fmt.Sprintf("%d%s", len(r.partly), listed(r.partly)))
	}
	if len(r.created)+len(r.updated)+len(r.partly) > 0 {
		r.writeResult(p)
		if runErr != nil {
			p.para(2, "Running again is safe: what was created is found and reused.")
		}
	} else {
		p.line("  Nothing was changed.")
	}
	p.labelled(2, 15, "Audit log", r.audit.path)
}

func listed(l []string) string {
	if len(l) == 0 {
		return ""
	}
	return " · " + strings.Join(l, ", ")
}

func (r *run) writeResult(p *printer) {
	s, err := exportSpace(context.WithoutCancel(r.ctx), r.writer.Client, r.sess.project.Key,
		export.Options{ToolVersion: r.opts.ToolVersion})
	if err == nil {
		var data []byte
		if data, err = spec.Write(s); err == nil {
			err = os.WriteFile(r.opts.Output, data, 0o600)
		}
	}
	if err != nil {
		p.labelled(2, 15, "Resulting spec", "not written: "+err.Error()+"; 'export' writes it from the space")
		return
	}
	p.labelled(2, 15, "Resulting spec", r.opts.Output+" (an export of the space, with IDs)")
}

// exportSpace writes the resulting spec; tests on sites that cannot answer
// a whole export replace it.
var exportSpace = export.Space

// writeProblem describes a failed write: what Jira answered (or what went
// wrong), and what to do. What the run did before is in the summary.
func writeProblem(title string, err error) Problem {
	pr := Problem{Title: title, Why: err.Error(),
		Fix: "Nothing more was sent in this step; the summary below lists what this run did. Once the cause is " +
			"fixed, run again: what was created is found and reused."}
	var apiErr *jira.APIError
	if errors.As(err, &apiErr) {
		pr.Request = apiErr.Method + " " + apiErr.Path
		pr.JiraAnswered = strings.TrimPrefix(apiErr.Error(), pr.Request+": ")
		pr.Why = apiErr.Hint()
	}
	return pr
}

// changesHere reports whether an item changes Jira in its own step (a
// switch an earlier step decided is made in step 25).
func changesHere(it steps.Item) bool {
	return (it.Kind == steps.Create || it.Kind == steps.Update) && it.Later == ""
}
