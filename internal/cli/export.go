package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/jerryjvl/jira-space-config/internal/export"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

func newExportCommand(env environment) *cobra.Command {
	var (
		conn   connection
		space  string
		output string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "export --site URL --space KEY [-o FILE]",
		Short: "Write a space's configuration as a spec (read-only)",
		Long: "export reads the configuration of one company-managed space and writes it as\n" +
			"a spec in canonical form, with IDs, to standard output or to --output.\n" +
			"It only reads from Jira.\n\n" +
			"Not exported: issue layouts, which the API cannot read. Anything else left\n" +
			"out is listed under notes: in the output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validSpaceKey(space); err != nil {
				return err
			}
			if output != "" && !force {
				if _, err := os.Stat(output); err == nil {
					return fmt.Errorf("%s already exists; use --force to overwrite it", output)
				} else if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
			client, err := conn.client(cmd.Context(), env)
			if err != nil {
				return err
			}
			s, err := export.Space(cmd.Context(), client, space, export.Options{ToolVersion: version})
			if err != nil {
				return err
			}
			// The export must pass the tool's own validation; if it does not,
			// that is a bug in export, so say so rather than writing silently.
			if diags := spec.Validate(s); diags.HasErrors() {
				for _, d := range diags.Sorted() {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning: export failed its own validation:", d.Format("export"))
				}
			}
			out, err := spec.Write(s)
			if err != nil {
				return err
			}
			if output == "" {
				_, err = cmd.OutOrStdout().Write(out)
			} else {
				err = os.WriteFile(output, out, 0o600)
			}
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Exported space %s: %d issue types, %d statuses, %d resolutions, %d priorities, %d screens, %d fields, %d workflows, %d field contexts, %d roles. %d requests made, all read-only.\n",
				space, len(s.Space.IssueTypes), len(s.Statuses), len(s.Resolutions), len(s.Priorities), len(s.Screens), len(s.Space.Fields.V), len(s.Workflows), len(s.Space.FieldContexts), len(s.Roles), client.Requests())
			return nil
		},
	}
	conn.addFlags(cmd)
	cmd.Flags().StringVar(&space, "space", "", "key of the space to export, e.g. VULN (required)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to this file instead of standard output")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite --output if it exists")
	_ = cmd.MarkFlagRequired("space")
	return cmd
}
