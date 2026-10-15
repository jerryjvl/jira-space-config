package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

func newValidateCommand() *cobra.Command {
	var canonical, verbose bool
	cmd := &cobra.Command{
		Use:   "validate FILE",
		Short: "Check a spec for errors without contacting Jira",
		Long: "validate reads a spec and reports every problem it can find offline, as\n" +
			"file:line:col messages: structure, unknown keys, rule and holder syntax, naming\n" +
			"rules, and references between the spec's own sections.\n\n" +
			"Names the spec uses without declaring (for example an existing status) must\n" +
			"already exist on the site; --verbose lists them.\n\n" +
			"With --canonical, the spec is printed in canonical form (the form export\n" +
			"writes) on standard output, and messages go to standard error.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := args[0]
			data, err := os.ReadFile(file) //nolint:gosec // G304: reading the file the operator named is the point
			if err != nil {
				return err
			}
			s, diags := spec.Parse(data)
			// Validate whatever was readable, so one run reports every problem;
			// only YAML that could not be read at all leaves nothing to check.
			readable := s.Pos.Line > 0
			if readable {
				diags = append(diags, spec.Validate(s)...)
			}

			report := cmd.OutOrStdout()
			if canonical {
				report = cmd.ErrOrStderr()
			}
			printDiagnostics(report, file, diags, verbose)

			if n := diags.Count(spec.SeverityError); n > 0 {
				return errors.New(util.Plural(n, "error", "errors") + " in " + file)
			}
			if canonical {
				out, err := spec.Write(s)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(out)
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&canonical, "canonical", false, "print the spec in canonical form")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "also list names that must already exist on the site")
	return cmd
}

func printDiagnostics(w io.Writer, file string, diags spec.Diagnostics, verbose bool) {
	infos := 0
	for _, d := range diags.Sorted() {
		if d.Severity == spec.SeverityInfo && !verbose {
			infos++
			continue
		}
		_, _ = fmt.Fprintln(w, d.Format(file))
	}
	errs, warns := diags.Count(spec.SeverityError), diags.Count(spec.SeverityWarning)
	summary := fmt.Sprintf("%s: %s, %s", file, util.Plural(errs, "error", "errors"), util.Plural(warns, "warning", "warnings"))
	if infos > 0 {
		summary += fmt.Sprintf("; %s not declared in the spec must already exist on the site (--verbose lists them)",
			util.Plural(infos, "name", "names"))
	}
	_, _ = fmt.Fprintln(w, summary)
}
