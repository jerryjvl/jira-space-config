package apply

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/jerryjvl/jira-space-config/internal/steps"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The text layout is what docs/cmdline.md shows; tests compare it as golden
// text.

const width = 100

// printer writes the run's output. Write errors are kept and reported once.
type printer struct {
	w            io.Writer
	colour       bool
	showRequests bool
	lastIntro    string // a step's introduction is shown only when it differs from the last one
	err          error
}

func (p *printer) line(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format+"\n", args...)
	}
}

func (p *printer) blank() { p.line("") }

// para writes text wrapped to the page, each line indented.
func (p *printer) para(indent int, text string) {
	for _, l := range wrap(text, width-indent) {
		p.line("%s%s", strings.Repeat(" ", indent), l)
	}
}

// labelled writes "label   text" lines, the text wrapped and aligned.
func (p *printer) labelled(indent, labelWidth int, label, text string) {
	pad := strings.Repeat(" ", indent+labelWidth)
	for i, l := range wrap(text, width-indent-labelWidth) {
		if i == 0 {
			p.line("%s%-*s%s", strings.Repeat(" ", indent), labelWidth, label, l)
		} else {
			p.line("%s%s", pad, l)
		}
	}
}

func wrap(text string, w int) []string {
	var out []string
	var cur string
	for _, word := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = word
		case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

var markers = map[steps.Kind]struct {
	text   string
	colour string // ANSI code
}{
	steps.Exists:  {"✓ exists", "32"},
	steps.Create:  {"+ create", "36"},
	steps.Update:  {"~ update", "33"},
	steps.Differs: {"✗ differs", "31"},
	steps.Error:   {"✗ error", "31"},
	steps.Blocked: {"■ blocked", "35"},
	steps.Manual:  {"? manual", "90"},
}

const markerWidth = 11

func (p *printer) marker(k steps.Kind) string {
	m := markers[k]
	text := m.text + strings.Repeat(" ", markerWidth-utf8.RuneCountInString(m.text))
	if p.colour {
		return "\x1b[" + m.colour + "m" + m.text + "\x1b[0m" + text[len(m.text):]
	}
	return text
}

// stepTitle writes "── Step 4 of 28 · Statuses ─────…".
func (p *printer) stepTitle(number int, title string) {
	head := fmt.Sprintf("── Step %d of %d · %s ", number, steps.Total, title)
	p.line("%s%s", head, strings.Repeat("─", max(3, width-utf8.RuneCountInString(head))))
}

// step writes one evaluated step.
func (p *printer) step(s *steps.Step, showRequests bool) {
	p.stepTitle(s.Number, s.Title)
	p.blank()
	if s.Intro != p.lastIntro {
		p.para(2, s.Intro)
		p.blank()
		p.lastIntro = s.Intro
	}
	// The name column fits the names up to 28 characters; longer ones get a
	// line of their own.
	nameWidth := 0
	for _, it := range s.Items {
		if n := utf8.RuneCountInString(it.Name); n <= 28 {
			nameWidth = max(nameWidth, n)
		}
	}
	detail := 2 + markerWidth + nameWidth + 2 // where the summary column starts
	for _, it := range s.Items {
		summary := it.Summary
		if it.Kind == steps.Create && it.SiteWide {
			summary += " · site-wide" // shared by every space on the site
		}
		lines := wrap(summary, width-detail)
		if utf8.RuneCountInString(it.Name) > nameWidth {
			// A name too long for the column gets a line of its own.
			p.line("  %s%s", p.marker(it.Kind), it.Name)
		} else {
			name := it.Name + strings.Repeat(" ", nameWidth-utf8.RuneCountInString(it.Name))
			p.line("  %s%s  %s", p.marker(it.Kind), name, first(lines))
			lines = rest(lines)
		}
		for _, l := range lines {
			p.line("%s%s", strings.Repeat(" ", detail), l)
		}
		attrWidth := 0
		for _, d := range it.Diffs {
			attrWidth = max(attrWidth, len(d.Attribute))
		}
		for _, d := range it.Diffs {
			p.diff(detail+2, attrWidth, d)
		}
		if it.Fix != "" {
			p.para(detail, "Fix: "+it.Fix)
		}
		if showRequests {
			for _, r := range it.Requests {
				p.line("%s%s", strings.Repeat(" ", detail+2), r)
			}
		}
	}
	for _, n := range s.Notes {
		p.blank()
		p.para(2, "Note: "+n)
	}
	p.blank()
}

func first(l []string) string {
	if len(l) == 0 {
		return ""
	}
	return l[0]
}

func rest(l []string) []string {
	if len(l) < 2 {
		return nil
	}
	return l[1:]
}

// counts describes a step's items for the overview: "2 to create · 3 exist".
func counts(s *steps.Step) string {
	exists, create := s.Count(steps.Exists), s.Count(steps.Create)
	if exists == len(s.Items) {
		return fmt.Sprintf("nothing to do (%s)", util.Plural(exists, "exists", "exist"))
	}
	var parts []string
	add := func(n int, text string) {
		if n > 0 {
			parts = append(parts, text)
		}
	}
	add(create, fmt.Sprintf("%d to create", create))
	add(exists, util.Plural(exists, "exists", "exist"))
	add(s.Count(steps.Differs), util.Plural(s.Count(steps.Differs), "differs", "differ"))
	add(s.Count(steps.Error), util.Plural(s.Count(steps.Error), "problem", "problems"))
	add(s.Count(steps.Update), fmt.Sprintf("%d to change", s.Count(steps.Update)))
	add(s.Count(steps.Blocked), fmt.Sprintf("%d blocked", s.Count(steps.Blocked)))
	add(s.Count(steps.Manual), fmt.Sprintf("%d manual", s.Count(steps.Manual)))
	return strings.Join(parts, " · ")
}

// Problem is a reason the run stops, in the shape every problem has: what
// failed, why, and how to fix it.
type Problem struct {
	Title string
	Lines []string // shown as they are, before the labelled lines
	// Labelled lines, in order: Request (shown with --show-requests only),
	// Jira answered, While, Why, Fix.
	Request, JiraAnswered, While, Why, Fix string
}

func (p *printer) problem(pr Problem) {
	if !p.showRequests {
		pr.Request = ""
	}
	p.line("✗ %s", pr.Title)
	p.blank()
	for _, l := range pr.Lines {
		p.line("  %s", l)
	}
	if len(pr.Lines) > 0 {
		p.blank()
	}
	for _, l := range []struct{ label, text string }{
		{"Request", pr.Request}, {"Jira answered", pr.JiraAnswered}, {"While", pr.While}, {"Why", pr.Why}, {"Fix", pr.Fix},
	} {
		if l.text != "" {
			p.labelled(2, 16, l.label, l.text)
		}
	}
	p.blank()
}

// diff writes one difference on a line, or, when that does not fit, the
// attribute and then each value wrapped on its own lines.
func (p *printer) diff(indent, attrWidth int, d steps.Diff) {
	pad := strings.Repeat(" ", indent)
	line := fmt.Sprintf("%-*s   spec: %s   Jira: %s", attrWidth, d.Attribute, d.Spec, d.Jira)
	if indent+utf8.RuneCountInString(line) <= width {
		p.line("%s%s", pad, line)
		return
	}
	p.line("%s%s", pad, d.Attribute)
	p.labelled(indent+2, 6, "spec:", d.Spec)
	p.labelled(indent+2, 6, "Jira:", d.Jira)
}
