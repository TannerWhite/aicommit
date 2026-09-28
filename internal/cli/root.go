package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newRoot(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "aicommit",
		Short:         "Write Conventional Commits messages with AI and split changes into atomic commits",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, args []string) error { return nil },
	}
	root.SetVersionTemplate("aicommit {{.Version}}\n")
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot(stdin, stdout, stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(stderr, "aicommit:", err)
		return 1
	}
	return 0
}
