package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jerryjvl/jira-space-config/internal/apply"
)

func newApplyCommand(env environment) *cobra.Command {
	var (
		conn         connection
		space        string
		dryRun       bool
		ignoreIDs    bool
		showRequests bool
		noColour     bool
		output       string
		allowUpdate  []string
		allowDelete  []string
		checkUsage   []string
		logDir       string
	)
	cmd := &cobra.Command{
		Use:   "apply [--dry-run] --site URL --space KEY SPEC",
		Short: "Apply a spec to a space, one confirmed step at a time (or --dry-run)",
		Long: "apply compares a spec with the site, one object type per step, and prints\n" +
			"what applying it would do: what exists and matches, what would be created,\n" +
			"and what must be fixed first, with how to fix it.\n\n" +
			"With --dry-run it evaluates all 28 steps, from site-wide objects to the\n" +
			"space's scheme switches, with the work item safety counts, and only reads.\n\n" +
			"Without it, each step with changes is shown and carried out only when you\n" +
			"proceed; it needs a terminal. Issue layouts, which the API cannot write,\n" +
			"are listed for you to set up by hand. It stops, unchanged, at a change\n" +
			"this tool cannot make.\n" +
			"Every write is logged; the resulting spec is written to --output.\n\n" +
			"The exit status is 1 when something must be fixed before the spec can be applied.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validSpaceKey(space); err != nil {
				return err
			}
			for _, a := range allowUpdate {
				if !slices.Contains(updateKinds, a) {
					return fmt.Errorf("--allow-update=%s: the kinds of change that can be allowed are: %s", a, strings.Join(updateKinds, ", "))
				}
			}
			for _, a := range checkUsage {
				if a != "permission_scheme" {
					return fmt.Errorf("--check-usage=%s: the kinds whose usage can be checked space by space are: permission_scheme", a)
				}
			}
			for _, a := range allowDelete {
				if !slices.Contains(deleteKinds, a) {
					return fmt.Errorf("--allow-delete=%s: the kinds of removal that can be allowed are: %s", a, strings.Join(deleteKinds, ", "))
				}
			}
			data, err := os.ReadFile(args[0]) //nolint:gosec // G304: reading the file the operator named is the point
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			runOpts := apply.Options{
				SpecFile: args[0], Spec: data, SpaceKey: space,
				IgnoreIDs: ignoreIDs, ShowRequests: showRequests, AllowUpdate: allowUpdate, AllowDelete: allowDelete, CheckUsage: checkUsage,
				Colour: !noColour && env.getenv("NO_COLOR") == "" && env.terminal(out), //nolint:misspell // the standard variable's name
			}
			if dryRun {
				client, err := conn.client(cmd.Context(), env)
				if err != nil {
					return err
				}
				return apply.DryRun(cmd.Context(), client, runOpts, out)
			}
			if !env.terminal(env.stdin) {
				return errors.New("a live apply asks before every step, so it needs a terminal; run it in one, or use --dry-run")
			}
			connect, err := conn.options(env)
			if err != nil {
				return err
			}
			if logDir == "" {
				if logDir, err = apply.StateDir(env.getenv); err != nil {
					return err
				}
			}
			if output == "" {
				output = strings.TrimSuffix(args[0], filepath.Ext(args[0])) + ".applied.yaml"
			}
			err = apply.Live(cmd.Context(), apply.LiveOptions{Options: runOpts, Connect: connect, In: env.stdin,
				StateDir: logDir, Output: output, ToolVersion: version}, out)
			// A *apply.Stopped error has been explained in the output; its message is
			// the one-line summary printed after it.
			return err
		},
	}
	conn.addFlags(cmd)
	cmd.Flags().StringVar(&space, "space", "", "key of the space the spec describes, e.g. VULN (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "evaluate and print the plan; change nothing")
	cmd.Flags().StringVarP(&output, "output", "o", "", "live: where the resulting spec is written (default <spec>.applied.yaml)")
	cmd.Flags().StringVar(&logDir, "log-dir", "", "live: where the lock and audit log are kept (default the user's state directory)")
	cmd.Flags().StringSliceVar(&allowUpdate, "allow-update", nil, "allow changes that widen access, by kind: role_members, notifications, permissions, issue_security, filters")
	cmd.Flags().StringSliceVar(&checkUsage, "check-usage", nil, "check usage the slow way, one request per space, by kind: permission_scheme")
	cmd.Flags().StringSliceVar(&allowDelete, "allow-delete", nil, "allow changes that remove, by kind: workflows (statuses, transitions), contexts (options, issue types, contexts of the space), issue_types, fields, priorities, notifications, permissions, issue_security (of the space), filters (viewers and editors)")
	cmd.Flags().BoolVar(&ignoreIDs, "ignore-ids", false, "find objects by name only, for a spec written for another site")
	cmd.Flags().BoolVar(&showRequests, "show-requests", false, "list the API call each action would make")
	cmd.Flags().BoolVar(&noColour, "no-colour", false, "no colour in the output (NO_COLOR is honoured too)") //nolint:misspell // the standard variable's name
	_ = cmd.MarkFlagRequired("space")
	return cmd
}

func isTerminal(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// updateKinds are the kinds of change that widens access --allow-update can allow.
var updateKinds = []string{"role_members", "notifications", "permissions", "issue_security", "filters"}

// deleteKinds are the kinds of removal --allow-delete can allow.
var deleteKinds = []string{"workflows", "contexts", "issue_types", "fields", "priorities", "notifications", "permissions", "issue_security", "filters"}
