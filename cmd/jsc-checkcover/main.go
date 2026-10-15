// Command jsc-checkcover is a development tool: it fails when a check in the
// tool is never made to fire by a test. A check is code that reports a
// problem (a diagnostic, an error it creates, an export note, a panic); one
// no test reaches may be trivially satisfied, so it is either tested or
// marked, in the code, with why no input can reach it:
//
//	// checkcover:unreachable json.Marshal cannot fail for these fixed types
//
// The marker goes in the comment directly above the block, or inside it. A marker on a
// block that tests do reach is reported too, so markers cannot go stale.
// Plain propagation of an error made elsewhere (return err) is not a check.
//
//	go test -coverpkg=./... -coverprofile=bin/cover.out ./...
//	go run ./cmd/jsc-checkcover bin/cover.out
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// problem matches the ways the tool reports a problem: diagnostics, errors
// it creates, notes, panics, and apply items that block or need the
// operator (an item's Kind set to Error, Differs, Blocked or Manual).
var problem = regexp.MustCompile(`\.(errorf|warnf|infof)\(|fmt\.Errorf\(|errors\.New\(|\b[er]\.note\(|notes = append\(|lookupFailed\(|panic\(|` +
	`Kind(\s*=\s*|:\s*)(Error|Differs|Blocked|Manual)\b`)

const marker = "checkcover:unreachable"

// skipped are development code, not the tool.
var skipped = []string{"cmd/jsc-record/", "cmd/jsc-scrub/", "cmd/jsc-sandbox/", "cmd/jsc-checkcover/", "internal/jira/jiratest/"}

type block struct {
	file       string
	start, end int
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: jsc-checkcover COVERPROFILE")
		os.Exit(2)
	}
	failures, err := run(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	for _, f := range failures {
		fmt.Println(f)
	}
	if len(failures) > 0 {
		fmt.Printf("\n%d finding(s). A check no test makes fire needs a test, or, if no input can reach it,\n"+
			"// %s <why>. A marker on a check that tests reach must go.\n", len(failures), marker)
		os.Exit(1)
	}
	fmt.Println("Every check is made to fire by a test, or marked unreachable with a reason.")
}

func run(profile string) ([]string, error) {
	module, err := modulePath()
	if err != nil {
		return nil, err
	}
	uncovered, err := uncoveredBlocks(profile, module)
	if err != nil {
		return nil, err
	}
	sources := map[string][]string{}
	lines := func(file string) ([]string, error) {
		if l, ok := sources[file]; ok {
			return l, nil
		}
		data, err := os.ReadFile(file) //nolint:gosec // G304: files named by the coverage profile of this module
		if err != nil {
			return nil, err
		}
		sources[file] = strings.Split(string(data), "\n")
		return sources[file], nil
	}

	var failures []string
	used := map[string]bool{} // file:line of markers that explain an uncovered check
	for _, b := range uncovered {
		src, err := lines(b.file)
		if err != nil {
			return nil, err
		}
		text := strings.Join(src[b.start-1:b.end], "\n")
		if !problem.MatchString(text) {
			continue
		}
		// The marker may be in the comment directly above the block, or inside it.
		first := b.start
		for first > 1 && strings.HasPrefix(strings.TrimSpace(src[first-2]), "//") {
			first--
		}
		explained := false
		for l := first; l <= b.end; l++ {
			if _, reason, ok := strings.Cut(src[l-1], marker); ok {
				used[b.file+":"+strconv.Itoa(l)] = true
				if strings.TrimSpace(reason) == "" {
					failures = append(failures, fmt.Sprintf("%s:%d: %s needs a reason", b.file, l, marker))
				}
				explained = true
			}
		}
		if !explained {
			failures = append(failures, fmt.Sprintf("%s:%d: %s", b.file, b.start, strings.TrimSpace(src[b.start-1])))
		}
	}

	// Every marker must explain an uncovered check.
	err = filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || isSkipped(path) {
			return err
		}
		src, err := lines(path)
		if err != nil {
			return err
		}
		for i, l := range src {
			if strings.Contains(l, "// "+marker) && !used[path+":"+strconv.Itoa(i+1)] {
				failures = append(failures, fmt.Sprintf("%s:%d: marked unreachable, but tests reach it or it reports no problem; remove the marker", path, i+1))
			}
		}
		return nil
	})
	sort.Strings(failures)
	return failures, err
}

// uncoveredBlocks reads a coverage profile. With -coverpkg each test binary
// reports every block, so a block counts as covered if any binary ran it.
func uncoveredBlocks(profile, module string) ([]block, error) {
	f, err := os.Open(profile) //nolint:gosec // G304: the profile the developer named
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	counts := map[block]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		// file.go:startLine.startCol,endLine.endCol statements count
		loc, rest, ok := strings.Cut(line, " ")
		fields := strings.Fields(rest)
		name, span, ok2 := strings.Cut(loc, ":")
		from, to, ok3 := strings.Cut(span, ",")
		if !ok || !ok2 || !ok3 || len(fields) != 2 {
			return nil, fmt.Errorf("%s: unexpected line %q", profile, line)
		}
		file, inModule := strings.CutPrefix(name, module+"/")
		if !inModule || isSkipped(file) {
			continue
		}
		start, err1 := strconv.Atoi(strings.Split(from, ".")[0])
		end, err2 := strconv.Atoi(strings.Split(to, ".")[0])
		count, err3 := strconv.Atoi(fields[1])
		if err := errors.Join(err1, err2, err3); err != nil {
			return nil, fmt.Errorf("%s: unexpected line %q: %w", profile, line, err)
		}
		b := block{file, start, end}
		counts[b] = max(counts[b], count)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	var out []block
	for b, n := range counts {
		if n == 0 {
			out = append(out, b)
		}
	}
	return out, nil
}

func modulePath() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", fmt.Errorf("run from the repository root: %w", err)
	}
	for _, l := range strings.Split(string(data), "\n") {
		if m, ok := strings.CutPrefix(l, "module "); ok {
			return strings.TrimSpace(m), nil
		}
	}
	return "", errors.New("go.mod has no module line")
}

func isSkipped(file string) bool {
	for _, s := range skipped {
		if strings.HasPrefix(file, s) {
			return true
		}
	}
	return false
}
