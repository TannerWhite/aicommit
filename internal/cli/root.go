package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TannerWhite/aicommit/internal/app"
	"github.com/TannerWhite/aicommit/internal/backend"
	"github.com/TannerWhite/aicommit/internal/config"
	"github.com/TannerWhite/aicommit/internal/git"
	"github.com/TannerWhite/aicommit/internal/rules"
	"github.com/TannerWhite/aicommit/internal/ui"
)

type flags struct {
	dryRun, yes, all, verbose bool
	backend, model, rules     string
	exclude                   []string
}

type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func loadConfig(f *flags) (config.Config, git.Runner, string, error) {
	r := git.Runner{}
	root, err := r.RepoRoot()
	if err != nil {
		return config.Config{}, r, "", errors.New("not inside a git repository")
	}
	r.Dir = root
	userDir, err := config.UserDir(os.Getenv)
	if err != nil {
		return config.Config{}, r, "", err
	}
	c, err := config.Load(userDir, root, os.Getenv, config.Overrides{Backend: f.backend, Model: f.model, Rules: f.rules, Exclude: f.exclude})
	return c, r, userDir, err
}

func ghToken() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	return strings.TrimSpace(string(out)), err
}

func newRoot(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	f := &flags{}
	root := &cobra.Command{
		Use:           "aicommit",
		Short:         "Write Conventional Commits messages with AI and split changes into atomic commits",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, r, userDir, err := loadConfig(f)
			if err != nil {
				return err
			}
			for _, w := range c.Warnings {
				fmt.Fprintln(stderr, "aicommit: warning:", w)
			}
			text, _, err := rules.Resolve(rules.Sources{Flag: c.RulesFlag, RepoKey: c.RepoRules, UserKey: c.UserRules, RepoRoot: r.Dir, UserDir: userDir})
			if err != nil {
				return err
			}
			b, err := backend.New(c, backend.Env{LookPath: exec.LookPath, Getenv: os.Getenv, GHToken: ghToken})
			if err != nil {
				return err
			}
			interactive := false
			if file, ok := stdin.(*os.File); ok {
				interactive = ui.IsTerminal(file)
			}
			a := &app.App{
				Git: r, Backend: b, Rules: text, Config: c, Out: stdout,
				UI: ui.New(stdin, stdout), Interactive: interactive,
				Opts: app.Options{DryRun: f.dryRun, Yes: f.yes, All: f.all, Verbose: f.verbose},
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt)
			defer signal.Stop(sig)
			done := make(chan struct{})
			defer close(done)
			go func() {
				select {
				case <-sig:
					cancel() // kills an in-flight backend call
					a.Interrupt()
					fmt.Fprintln(stderr, "aicommit: interrupted")
					os.Exit(2)
				case <-done:
				}
			}()
			err = a.Run(ctx)
			if errors.Is(err, app.ErrQuit) {
				fmt.Fprintln(stderr, "aicommit: quit")
				return exitError{2}
			}
			if errors.Is(err, app.ErrNoTTY) {
				fmt.Fprintln(stderr, "aicommit:", err)
				return exitError{2}
			}
			return err
		},
	}
	root.SetVersionTemplate("aicommit {{.Version}}\n")
	pf := root.PersistentFlags()
	pf.StringVar(&f.backend, "backend", "", "backend: command | openai")
	pf.StringVar(&f.model, "model", "", "model to use")
	pf.StringVar(&f.rules, "rules", "", "rules file for this run")
	pf.StringArrayVar(&f.exclude, "exclude", nil, "never send matching file contents (repeatable)")
	fl := root.Flags()
	fl.BoolVarP(&f.dryRun, "dry-run", "n", false, "show the proposal, commit nothing")
	fl.BoolVarP(&f.yes, "yes", "y", false, "accept every proposal without prompting")
	fl.BoolVarP(&f.all, "all", "a", false, "ignore the index and split all changes")
	fl.BoolVarP(&f.verbose, "verbose", "v", false, "print prompts and backend details")

	root.AddCommand(&cobra.Command{
		Use:   "config",
		Short: "Print the merged configuration and where each value came from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, _, err := loadConfig(f)
			if err != nil {
				return err
			}
			for _, w := range c.Warnings {
				fmt.Fprintln(stderr, "aicommit: warning:", w)
			}
			fmt.Fprint(stdout, c.Describe())
			return nil
		},
	})
	root.AddCommand(newInit(stdout))
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot(stdin, stdout, stderr)
	root.SetArgs(args)
	err := root.Execute()
	var ee exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.code
	default:
		fmt.Fprintln(stderr, "aicommit:", err)
		return 1
	}
}
