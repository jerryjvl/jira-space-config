package instructions

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestGolden renders specs that between them use every part of the format.
func TestGolden(t *testing.T) {
	for _, file := range []string{"../spec/testdata/full.yaml", "testdata/extra.yaml", "testdata/own.yaml"} {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			s, diags := spec.Parse(data)
			diags = append(diags, spec.Validate(s)...)
			if diags = diags.WithoutAPIOnly(); diags.HasErrors() {
				t.Fatalf("%s is not valid: %v", file, diags)
			}
			got := Write(s, Options{Source: filepath.Base(file), ToolVersion: "test"})
			golden := filepath.Join("testdata", strings.TrimSuffix(filepath.Base(file), ".yaml")+".golden.md")
			if *update {
				if err := os.WriteFile(golden, got, 0o600); err != nil { //nolint:gosec // G703: the package's own golden files
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("instructions differ from %s (go test ./internal/instructions -update to accept)", golden)
			}
		})
	}
}

// TestTableLeavesOutEmptyColumns keeps tables to the values there are.
func TestTableLeavesOutEmptyColumns(t *testing.T) {
	d := &doc{s: &spec.Spec{Space: &spec.Space{Key: "T"}}}
	d.table([]string{"A", "B", "C"}, [][]string{{"1", "", "x|y"}, {"2"}})
	want := "| A | C |\n| --- | --- |\n| 1 | x\\|y |\n| 2 |  |\n\n"
	if d.b.String() != want {
		t.Errorf("table:\n%q\nwant\n%q", d.b.String(), want)
	}
}
