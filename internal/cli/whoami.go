package cli

import (
	"github.com/spf13/cobra"

	"github.com/jerryjvl/jira-space-config/internal/whoami"
)

func newWhoamiCommand(env environment) *cobra.Command {
	var (
		conn  connection
		space string
	)
	cmd := &cobra.Command{
		Use:   "whoami --site URL [--space KEY]",
		Short: "Check credentials and show the account's relevant permissions",
		Long: "whoami signs in with JIRA_EMAIL and JIRA_API_TOKEN and shows the account,\n" +
			"the site and its plan, and whether the account has the permissions this\n" +
			"tool needs. With --space it also checks that space. Read-only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validSpaceKey(space); err != nil {
				return err
			}
			client, err := conn.client(cmd.Context(), env)
			if err != nil {
				return err
			}
			report, err := whoami.Run(cmd.Context(), client, space)
			if err != nil {
				return err
			}
			return report.Render(cmd.OutOrStdout())
		},
	}
	conn.addFlags(cmd)
	cmd.Flags().StringVar(&space, "space", "", "space key to check, e.g. VULN")
	return cmd
}
