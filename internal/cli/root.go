// Package cli wires the command-line interface. It parses flags and hands
// work to other packages; it contains no Jira logic of its own.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X .../internal/cli.version=…".
var version = "dev"

// environment is what commands take from the outside world. Tests replace it.
type environment struct {
	getenv    func(string) string
	transport http.RoundTripper // nil means the default HTTP transport
	// stdin is where a live apply reads the operator's answers; terminal
	// says whether it is a terminal (tests replace both).
	stdin    io.Reader
	terminal func(any) bool
}

// Execute runs the command line with the given arguments and output streams.
// Errors are printed to stderr, with a hint where one is known, before being
// returned.
func Execute(args []string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return execute(ctx, args, stdout, stderr, environment{getenv: os.Getenv, stdin: os.Stdin, terminal: isTerminal})
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) error {
	root := newRootCommand(env)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		printError(stderr, err)
		return err
	}
	return nil
}

// hinter is implemented by errors that know how the operator can fix them.
type hinter interface{ Hint() string }

func printError(w io.Writer, err error) {
	_, _ = fmt.Fprintln(w, "Error:", err)
	var h hinter
	if errors.As(err, &h) && h.Hint() != "" {
		_, _ = fmt.Fprintln(w, "Hint: ", h.Hint())
	}
}

func newRootCommand(env environment) *cobra.Command {
	root := &cobra.Command{
		Use:   "jira-space-config",
		Short: "Describe a Jira Cloud company-managed space as YAML",
		Long: "jira-space-config describes the configuration of one existing Jira Cloud\n" +
			"company-managed space in a declarative YAML file.\n\n" +
			"Credentials are read from JIRA_EMAIL and JIRA_API_TOKEN only. The site is\n" +
			"always given explicitly with --site.",
		Version: version,
		Args:    cobra.NoArgs,
		// A Run function makes cobra validate Args, so stray words are
		// reported as unknown commands instead of being ignored.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newWhoamiCommand(env), newValidateCommand(), newExportCommand(env), newApplyCommand(env))
	return root
}
