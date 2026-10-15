package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Severity of a diagnostic.
type Severity int

// Severities.
const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityInfo
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	}
	return "info"
}

// Diagnostic is one finding about a file.
type Diagnostic struct {
	Severity Severity
	Pos      Pos
	Path     string // e.g. space.issue_types[0].workflow
	Message  string
	// APIOnly marks an error that holds only because Jira's API cannot do
	// what the spec asks; done by hand in Jira, it can be (instructions).
	APIOnly bool
}

// Format renders the diagnostic as file:line:col: severity: path: message.
func (d Diagnostic) Format(file string) string {
	loc := file
	if d.Pos.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", file, d.Pos.Line, d.Pos.Col)
	}
	if d.Path != "" {
		return fmt.Sprintf("%s: %s: %s: %s", loc, d.Severity, d.Path, d.Message)
	}
	return fmt.Sprintf("%s: %s: %s", loc, d.Severity, d.Message)
}

// Diagnostics is a list of findings.
type Diagnostics []Diagnostic

func (ds *Diagnostics) add(sev Severity, pos Pos, path, format string, args ...any) {
	*ds = append(*ds, Diagnostic{Severity: sev, Pos: pos, Path: path, Message: fmt.Sprintf(format, args...)})
}

func (ds *Diagnostics) errorf(pos Pos, path, format string, args ...any) {
	ds.add(SeverityError, pos, path, format, args...)
}

// apiErrorf reports what apply cannot do because Jira's API cannot.
func (ds *Diagnostics) apiErrorf(pos Pos, path, format string, args ...any) {
	ds.add(SeverityError, pos, path, format, args...)
	(*ds)[len(*ds)-1].APIOnly = true
}

// WithoutAPIOnly returns the diagnostics that hold whoever sets the space up.
func (ds Diagnostics) WithoutAPIOnly() Diagnostics {
	var out Diagnostics
	for _, d := range ds {
		if !d.APIOnly {
			out = append(out, d)
		}
	}
	return out
}

func (ds *Diagnostics) warnf(pos Pos, path, format string, args ...any) {
	ds.add(SeverityWarning, pos, path, format, args...)
}

func (ds *Diagnostics) infof(pos Pos, path, format string, args ...any) {
	ds.add(SeverityInfo, pos, path, format, args...)
}

// HasErrors reports whether any diagnostic is an error.
func (ds Diagnostics) HasErrors() bool {
	for _, d := range ds {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Count returns the number of diagnostics of a severity.
func (ds Diagnostics) Count(sev Severity) int {
	n := 0
	for _, d := range ds {
		if d.Severity == sev {
			n++
		}
	}
	return n
}

// Sorted returns the diagnostics in file order, errors first on the same line.
func (ds Diagnostics) Sorted() Diagnostics {
	out := append(Diagnostics(nil), ds...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Pos.Line != b.Pos.Line {
			return a.Pos.Line < b.Pos.Line
		}
		if a.Pos.Col != b.Pos.Col {
			return a.Pos.Col < b.Pos.Col
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Message < b.Message
	})
	return out
}

// Error makes Diagnostics usable as an error (errors only).
func (ds Diagnostics) Error() string {
	var lines []string
	for _, d := range ds.Sorted() {
		if d.Severity == SeverityError {
			lines = append(lines, d.Format("spec"))
		}
	}
	return strings.Join(lines, "\n")
}

// suggest returns the candidate closest to s, if it is close enough to be a
// likely typo.
func suggest(s string, candidates []string) string {
	best, bestDist := "", len(s)/2+2
	for _, c := range candidates {
		if d := editDistance(s, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
