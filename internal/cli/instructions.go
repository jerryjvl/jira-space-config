package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jerryjvl/jira-space-config/internal/instructions"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

func newInstructionsCommand() *cobra.Command {
	var (
		output string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "instructions FILE [-o FILE]",
		Short: "Write the steps to set a spec's space up by hand, as Markdown",
		Long: "instructions reads a spec and writes, in Markdown, the steps to set its space\n" +
			"up by hand in Jira: one section per kind of object, in the order apply works,\n" +
			"each with where in Jira it is done and a table of the values to enter.\n\n" +
			"It does not contact Jira, so it works where the tool cannot reach the site.\n" +
			"What the spec reuses is assumed to exist and is only named; what it ensures\n" +
			"is to be created if missing; the rest is to be created. A spec with errors\n" +
			"is refused, as validate reports them, except those that hold only because\n" +
			"Jira's API cannot do something a person can (an issue type above Epic, an\n" +
			"app's field type): those are listed to be created by hand.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := args[0]
			if output != "" && !force {
				if _, err := os.Stat(output); err == nil {
					return fmt.Errorf("%s already exists; use --force to overwrite it", output)
				} else if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
			data, err := os.ReadFile(file) //nolint:gosec // G304: reading the file the operator named is the point
			if err != nil {
				return err
			}
			s, diags := spec.Parse(data)
			if s.Pos.Line > 0 {
				diags = append(diags, spec.Validate(s)...)
			}
			// What only the API cannot do is still done by hand.
			diags = diags.WithoutAPIOnly()
			if n := diags.Count(spec.SeverityError); n > 0 {
				printDiagnostics(cmd.ErrOrStderr(), file, diags, false)
				return errors.New(util.Plural(n, "error", "errors") + " in " + file)
			}
			out := instructions.Write(s, instructions.Options{Source: filepath.Base(file), ToolVersion: version})
			if output == "" {
				_, err = cmd.OutOrStdout().Write(out)
				return err
			}
			if err := os.WriteFile(output, out, 0o600); err != nil { //nolint:gosec // G703: writing the file the operator named is the point
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Wrote the instructions for space %s to %s.\n", s.Space.Key, output)
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to this file instead of standard output")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite --output if it exists")
	return cmd
}
