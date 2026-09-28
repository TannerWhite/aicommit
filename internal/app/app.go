package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/TannerWhite/aicommit/internal/backend"
	"github.com/TannerWhite/aicommit/internal/changes"
	"github.com/TannerWhite/aicommit/internal/config"
	"github.com/TannerWhite/aicommit/internal/git"
	"github.com/TannerWhite/aicommit/internal/plan"
	"github.com/TannerWhite/aicommit/internal/prompt"
	"github.com/TannerWhite/aicommit/internal/ui"
)

var (
	ErrQuit  = errors.New("quit")
	ErrNoTTY = errors.New("stdin is not a terminal; pass --yes to accept proposals without prompting")
)

type Options struct{ DryRun, Yes, All, Verbose bool }

type App struct {
	Git         git.Runner
	Backend     backend.Backend
	Rules       string
	Config      config.Config
	UI          *ui.Prompter
	Out         io.Writer
	Interactive bool
	Opts        Options

	opMu        sync.Mutex // held for a group's whole stage/check/commit sequence
	interrupted bool       // set by Interrupt; guarded by opMu
	mu          sync.Mutex // guards the fields below
	printed     bool
	skipped     []string
	pending     []string // paths staged by aicommit and not yet committed
	all         []string // every path in the split change set
	done        map[string]bool
	committed   []string
	cleared     bool
}

func (a *App) Run(ctx context.Context) error {
	if !a.Interactive && !a.Opts.Yes && !a.Opts.DryRun {
		return ErrNoTTY
	}
	out, err := a.Git.Run(nil, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return err
	}
	st, err := git.ParseStatusV2(out)
	if err != nil {
		return err
	}
	for _, e := range st {
		if e.Unmerged {
			return fmt.Errorf("unresolved merge conflict in %s; resolve it before running aicommit", e.Path)
		}
	}
	hasStaged, err := a.Git.HasStaged()
	if err != nil {
		return err
	}
	if hasStaged && !a.Opts.All {
		return a.single(ctx)
	}
	return a.split(ctx, st)
}

func (a *App) changeOpts(staged bool) changes.Options {
	return changes.Options{Staged: staged, MaxFileBytes: a.Config.MaxFileBytes, MaxTotalBytes: a.Config.MaxTotalBytes, Exclude: a.Config.Exclude}
}

func (a *App) generate(ctx context.Context, p string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.Config.Timeout)
	defer cancel()
	if a.Opts.Verbose {
		fmt.Fprintf(a.Out, "--- backend: %s ---\n%s\n--- end prompt ---\n", a.Backend.Name(), p)
	}
	return a.Backend.Generate(ctx, p)
}

func (a *App) message(ctx context.Context, body, feedback string) (string, error) {
	raw, err := a.generate(ctx, prompt.Single(a.Rules, body, feedback))
	if err != nil {
		return "", err
	}
	msg := backend.Clean(raw)
	if subject(msg) == "" {
		return "", errors.New("backend returned an empty commit message")
	}
	return msg + "\n", nil
}

func subject(msg string) string {
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(msg), "\n", 2)[0])
}

func (a *App) choose(prompt, valid string) (rune, error) {
	if a.Opts.Yes {
		return 'a', nil
	}
	c, err := a.UI.Choose(prompt, valid)
	if err != nil {
		return 0, ErrQuit
	}
	return c, nil
}

func (a *App) single(ctx context.Context) error {
	out, err := a.Git.Run(nil, "diff", "--cached", "-z", "--name-status", "-M")
	if err != nil {
		return err
	}
	st, err := git.ParseNameStatusZ(out)
	if err != nil {
		return err
	}
	set, err := changes.Build(a.Git, st, a.changeOpts(true))
	if err != nil {
		return err
	}
	body := set.Render()
	msg, err := a.message(ctx, body, "")
	if err != nil {
		return err
	}
	for {
		fmt.Fprintf(a.Out, "\n----- Proposed commit message -----\n%s------------------------------------\n", msg)
		if a.Opts.DryRun {
			return nil
		}
		c, err := a.choose("[a]ccept, [e]dit, [r]egenerate, [q]uit: ", "aerq")
		if err != nil {
			return err
		}
		switch c {
		case 'a', 'e':
			sha, output, err := a.Git.Commit(msg, c == 'e')
			if err != nil {
				fmt.Fprint(a.Out, output)
				return err
			}
			fmt.Fprintf(a.Out, "aicommit: committed %s %s\n", sha, subject(msg))
			return nil
		case 'r':
			fb, err := a.UI.Line("Feedback for regeneration: ")
			if err != nil {
				return ErrQuit
			}
			if msg, err = a.message(ctx, body, fb); err != nil {
				return err
			}
		case 'q':
			return ErrQuit
		}
	}
}

func (a *App) split(ctx context.Context, st []git.StatusEntry) error {
	if len(st) == 0 {
		fmt.Fprintln(a.Out, "aicommit: nothing to commit (working tree clean)")
		return nil
	}
	set, err := changes.Build(a.Git, st, a.changeOpts(false))
	if err != nil {
		return err
	}
	p, err := a.makePlan(ctx, set)
	if err != nil {
		return err
	}
	printPlan(a.Out, p)
	if a.Opts.DryRun {
		return nil
	}
	a.mu.Lock()
	a.all, a.done = set.Paths(), map[string]bool{}
	a.mu.Unlock()
	defer a.printSummary()
	for i, c := range p.Commits {
		if err := a.group(ctx, set, i, len(p.Commits), c); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) makePlan(ctx context.Context, set changes.Set) (plan.Plan, error) {
	var problems, last string
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := a.generate(ctx, prompt.Split(a.Rules, set.Render(), problems))
		if err != nil {
			return plan.Plan{}, err
		}
		last = raw
		p, perr := plan.Parse(raw)
		var errs []string
		if perr != nil {
			errs = []string{perr.Error()}
		} else {
			errs = plan.Validate(p, set.Paths())
		}
		if len(errs) == 0 {
			return p, nil
		}
		problems = "- " + strings.Join(errs, "\n- ")
	}
	return plan.Plan{}, fmt.Errorf("model returned an invalid commit plan:\n%s\n--- raw output ---\n%s", problems, last)
}

func printPlan(w io.Writer, p plan.Plan) {
	fmt.Fprintf(w, "\n===== Proposed commit plan (%d commit(s)) =====\n", len(p.Commits))
	for i, c := range p.Commits {
		fmt.Fprintf(w, "\n--- Commit %d/%d ---\nFiles:\n", i+1, len(p.Commits))
		for _, f := range c.Files {
			fmt.Fprintf(w, "  - %s\n", f)
		}
		fmt.Fprintf(w, "Message:\n  %s\n", strings.ReplaceAll(strings.TrimSpace(c.Message), "\n", "\n  "))
	}
	fmt.Fprintln(w, "================================================")
}

func stagePaths(set changes.Set, files []string) []string {
	var out []string
	for _, f := range files {
		out = append(out, f)
		if e, ok := set.Find(f); ok && e.OrigPath != "" {
			out = append(out, e.OrigPath)
		}
	}
	return out
}

func subsetOf(set changes.Set, files []string) changes.Set {
	var s changes.Set
	for _, f := range files {
		if e, ok := set.Find(f); ok {
			s.Entries = append(s.Entries, e)
		}
	}
	return s
}

func (a *App) group(ctx context.Context, set changes.Set, i, n int, c plan.Commit) error {
	msg := strings.TrimSpace(c.Message) + "\n"
	paths := stagePaths(set, c.Files)
	for {
		fmt.Fprintf(a.Out, "\n----- Commit %d/%d -----\nFiles: %s\n%s------------------------------------\n", i+1, n, strings.Join(c.Files, ", "), msg)
		ch, err := a.choose("[a]ccept, [e]dit, [r]egenerate, [s]kip, [q]uit: ", "aersq")
		if err != nil {
			return err
		}
		switch ch {
		case 's':
			fmt.Fprintf(a.Out, "aicommit: skipped commit %d, files left unstaged\n", i+1)
			a.mu.Lock()
			a.skipped = append(a.skipped, fmt.Sprintf("commit %d: %s", i+1, subject(msg)))
			a.mu.Unlock()
			return nil
		case 'q':
			return ErrQuit
		case 'r':
			fb, err := a.UI.Line("Feedback for regeneration: ")
			if err != nil {
				return ErrQuit
			}
			if msg, err = a.message(ctx, subsetOf(set, c.Files).Render(), fb); err != nil {
				return err
			}
		case 'a', 'e':
			ok, err := a.commitGroup(msg, paths, c.Files, ch == 'e')
			if err != nil || ok {
				return err
			}
			if a.Opts.Yes {
				return fmt.Errorf("commit %d failed; its staging was undone", i+1)
			}
			fmt.Fprintln(a.Out, "aicommit: commit failed and staging was undone; [a]ccept retries")
		}
	}
}

func (a *App) commitGroup(msg string, paths, files []string, edit bool) (bool, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.interrupted {
		return false, ErrQuit
	}
	if a.Opts.All && !a.cleared {
		if err := a.Git.RestoreAllStaged(); err != nil {
			return false, err
		}
	}
	a.cleared = true
	pre, err := a.Git.StagedPaths()
	if err != nil {
		return false, err
	}
	if len(pre) > 0 {
		return false, fmt.Errorf("index is not empty (%s); something was staged outside aicommit, stopping", strings.Join(pre, ", "))
	}
	a.setPending(paths)
	if err := a.Git.Add(paths); err != nil {
		a.undo()
		return false, err
	}
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[p] = true
	}
	post, err := a.Git.StagedPaths()
	if err != nil {
		a.undo()
		return false, err
	}
	for _, p := range post {
		if !allowed[p] {
			a.undo()
			return false, fmt.Errorf("unexpected staged path %q, stopping", p)
		}
	}
	sha, output, err := a.Git.Commit(msg, edit)
	if err != nil {
		fmt.Fprint(a.Out, output)
		fmt.Fprintln(a.Out, "aicommit:", err)
		a.undo()
		return false, nil
	}
	a.mu.Lock()
	a.pending = nil
	a.committed = append(a.committed, sha+" "+subject(msg))
	for _, f := range files {
		a.done[f] = true
	}
	a.mu.Unlock()
	return true, nil
}

func (a *App) setPending(p []string) {
	a.mu.Lock()
	a.pending = p
	a.mu.Unlock()
}

func (a *App) undo() {
	a.mu.Lock()
	p := a.pending
	a.pending = nil
	a.mu.Unlock()
	if len(p) > 0 {
		if err := a.Git.RestoreStaged(p); err != nil {
			fmt.Fprintln(a.Out, "aicommit: failed to unstage:", err)
		}
	}
}

// Interrupt undoes the in-progress group's staging and prints the summary.
// It waits for any in-flight git operation of the current group to finish,
// and no later group touches the index once it has run.
func (a *App) Interrupt() {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.interrupted = true
	a.undo()
	a.printSummary()
}

func (a *App) printSummary() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done == nil || a.printed {
		return
	}
	a.printed = true
	fmt.Fprintln(a.Out, "\n===== aicommit summary =====")
	fmt.Fprintln(a.Out, "Committed:")
	if len(a.committed) == 0 {
		fmt.Fprintln(a.Out, "  (none)")
	}
	for _, c := range a.committed {
		fmt.Fprintf(a.Out, "  %s\n", c)
	}
	if len(a.skipped) > 0 {
		fmt.Fprintln(a.Out, "Skipped:")
		for _, s := range a.skipped {
			fmt.Fprintf(a.Out, "  %s\n", s)
		}
	}
	var left []string
	for _, p := range a.all {
		if !a.done[p] {
			left = append(left, p)
		}
	}
	if len(left) > 0 {
		fmt.Fprintf(a.Out, "Left uncommitted: %s\n", strings.Join(left, ", "))
	}
}
