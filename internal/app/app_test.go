package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TannerWhite/aicommit/internal/changes"
	"github.com/TannerWhite/aicommit/internal/config"
	"github.com/TannerWhite/aicommit/internal/git"
	"github.com/TannerWhite/aicommit/internal/ui"
)

type fake struct {
	responses []string
	prompts   []string
}

func (f *fake) Name() string { return "fake" }
func (f *fake) Generate(_ context.Context, p string) (string, error) {
	f.prompts = append(f.prompts, p)
	if len(f.responses) == 0 {
		return "", errors.New("fake: no more responses")
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return r, nil
}

func setup(t *testing.T) git.Runner {
	t.Helper()
	r := git.Runner{Dir: t.TempDir()}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}, {"config", "commit.gpgsign", "false"}, {"config", "core.hooksPath", ".git/hooks"}} {
		if _, err := r.Run(nil, a...); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func write(t *testing.T, r git.Runner, name, body string) {
	t.Helper()
	os.WriteFile(filepath.Join(r.Dir, name), []byte(body), 0o644)
}

func hook(t *testing.T, r git.Runner, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks not portable to windows")
	}
	p := filepath.Join(r.Dir, ".git", "hooks", name)
	os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755)
}

func newApp(r git.Runner, b *fake, input string, o Options) (*App, *bytes.Buffer) {
	var out bytes.Buffer
	return &App{
		Git: r, Backend: b, Rules: "RULES", Out: &out, Opts: o,
		Config:      config.Config{MaxFileBytes: 100000, MaxTotalBytes: 400000, Timeout: 5 * time.Second, Exclude: changes.DefaultExcludes},
		UI:          ui.New(strings.NewReader(input), &out),
		Interactive: input != "",
	}, &out
}

func log(t *testing.T, r git.Runner) []string {
	out, err := r.Run(nil, "log", "--format=%s")
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(out), "\n")
}

func staged(t *testing.T, r git.Runner) []string {
	s, err := r.StagedPaths()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSingleYes(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	r.Add([]string{"a.txt"})
	a, _ := newApp(r, &fake{responses: []string{"```\nfeat: add a\n```"}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log(t, r); got[0] != "feat: add a" {
		t.Fatalf("log %v", got)
	}
}

func TestSingleRegenerateThenAccept(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	r.Add([]string{"a.txt"})
	b := &fake{responses: []string{"feat: first", "feat: second"}}
	a, _ := newApp(r, b, "r\nshorter\na\n", Options{})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.prompts[1], "shorter") || log(t, r)[0] != "feat: second" {
		t.Fatalf("feedback not used: %v", log(t, r))
	}
}

func TestSingleEditUsesEditor(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	r.Add([]string{"a.txt"})
	t.Setenv("GIT_EDITOR", "true")
	a, _ := newApp(r, &fake{responses: []string{"feat: edited"}}, "e\n", Options{})
	if err := a.Run(context.Background()); err != nil || log(t, r)[0] != "feat: edited" {
		t.Fatalf("err %v log %v", err, log(t, r))
	}
}

func TestQuitAndNoTTY(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	r.Add([]string{"a.txt"})
	a, _ := newApp(r, &fake{responses: []string{"feat: x"}}, "q\n", Options{})
	if err := a.Run(context.Background()); !errors.Is(err, ErrQuit) {
		t.Fatalf("want ErrQuit, got %v", err)
	}
	a, _ = newApp(r, &fake{}, "", Options{})
	if err := a.Run(context.Background()); !errors.Is(err, ErrNoTTY) {
		t.Fatalf("want ErrNoTTY, got %v", err)
	}
}

const twoGroups = `{"commits":[{"files":["a.txt"],"message":"feat: add a"},{"files":["b.txt"],"message":"docs: add b"}]}`

func TestSplitYes(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	a, out := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log(t, r); len(got) != 2 || got[0] != "docs: add b" || got[1] != "feat: add a" {
		t.Fatalf("log %v", got)
	}
	if !strings.Contains(out.String(), "Committed:") {
		t.Fatalf("no summary: %s", out.String())
	}
}

func TestSplitRetryThenFail(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	bad := `{"commits":[{"files":["a.txt"],"message":"feat: a"}]}`
	b := &fake{responses: []string{bad, twoGroups}}
	a, _ := newApp(r, b, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.prompts[1], `"b.txt" is missing`) {
		t.Fatal("retry prompt lacks validation errors")
	}
	r2 := setup(t)
	write(t, r2, "a.txt", "a\n")
	write(t, r2, "b.txt", "b\n")
	a, _ = newApp(r2, &fake{responses: []string{bad, "not json"}}, "", Options{Yes: true})
	err := a.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "raw output") || log(t, r2) != nil {
		t.Fatalf("want failure with nothing committed, got %v %v", err, log(t, r2))
	}
}

func TestSplitSkip(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	a, out := newApp(r, &fake{responses: []string{twoGroups}}, "s\na\n", Options{})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log(t, r); len(got) != 1 || got[0] != "docs: add b" {
		t.Fatalf("log %v", got)
	}
	if !strings.Contains(out.String(), "Skipped:\n  commit 1: feat: add a") {
		t.Fatalf("no skipped line: %s", out.String())
	}
	if !strings.Contains(out.String(), "Left uncommitted: a.txt") {
		t.Fatalf("summary: %s", out.String())
	}
}

func TestHookRejectionUndoesStaging(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	hook(t, r, "commit-msg", "echo rejected by hook; exit 1")
	a, out := newApp(r, &fake{responses: []string{`{"commits":[{"files":["a.txt"],"message":"feat: a"}]}`}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if len(staged(t, r)) != 0 || !strings.Contains(out.String(), "rejected by hook") {
		t.Fatalf("staged %v out %s", staged(t, r), out.String())
	}
}

func TestIndexChangedMidRunAborts(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	hook(t, r, "post-commit", "echo x > sneaky.txt && git add sneaky.txt")
	a, _ := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{Yes: true})
	err := a.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "index is not empty") || len(log(t, r)) != 1 {
		t.Fatalf("want abort after first commit, got %v %v", err, log(t, r))
	}
}

func TestMergeConflictRefused(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "base\n")
	r.Add([]string{"a.txt"})
	r.Commit("chore: base\n", false)
	r.Run(nil, "checkout", "-q", "-b", "other")
	write(t, r, "a.txt", "other\n")
	r.Run(nil, "commit", "-qam", "chore: other")
	r.Run(nil, "checkout", "-q", "main")
	write(t, r, "a.txt", "main\n")
	r.Run(nil, "commit", "-qam", "chore: main")
	r.Run(nil, "merge", "-q", "other")
	b := &fake{}
	a, _ := newApp(r, b, "", Options{Yes: true, All: true})
	err := a.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "merge conflict") || len(b.prompts) != 0 {
		t.Fatalf("got %v, prompts %d", err, len(b.prompts))
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	a, out := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{DryRun: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if log(t, r) != nil || len(staged(t, r)) != 0 || !strings.Contains(out.String(), "docs: add b") {
		t.Fatal("dry run modified repo or did not print plan")
	}
}

func TestAllWithPartialIndex(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	r.Add([]string{"a.txt"})
	a, _ := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{Yes: true, All: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log(t, r); len(got) != 2 {
		t.Fatalf("log %v", got)
	}
}

func TestSplitRenameStagesBothSides(t *testing.T) {
	r := setup(t)
	write(t, r, "old.txt", strings.Repeat("same\n", 20))
	r.Add([]string{"old.txt"})
	r.Commit("chore: init\n", false)
	os.Rename(filepath.Join(r.Dir, "old.txt"), filepath.Join(r.Dir, "new.txt"))
	// Working-tree renames appear as a deletion plus an untracked file.
	plan := `{"commits":[{"files":["old.txt","new.txt"],"message":"refactor: rename old to new"}]}`
	a, _ := newApp(r, &fake{responses: []string{plan}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := r.Run(nil, "status", "--porcelain")
	if strings.TrimSpace(st) != "" {
		t.Fatalf("tree not clean: %q", st)
	}
}

func TestMergeConflictRefusedSingleMode(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "base\n")
	r.Add([]string{"a.txt"})
	r.Commit("chore: base\n", false)
	r.Run(nil, "checkout", "-q", "-b", "other")
	write(t, r, "a.txt", "other\n")
	r.Run(nil, "commit", "-qam", "chore: other")
	r.Run(nil, "checkout", "-q", "main")
	write(t, r, "a.txt", "main\n")
	r.Run(nil, "commit", "-qam", "chore: main")
	r.Run(nil, "merge", "-q", "other")
	write(t, r, "c.txt", "resolved\n")
	r.Add([]string{"c.txt"})
	b := &fake{responses: []string{"feat: x"}}
	a, _ := newApp(r, b, "", Options{Yes: true})
	err := a.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "merge conflict") || len(b.prompts) != 0 {
		t.Fatalf("got %v, prompts %d", err, len(b.prompts))
	}
}

func TestInterruptAfterRunPrintsSummaryOnce(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	a, out := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{Yes: true})
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.Interrupt()
	if n := strings.Count(out.String(), "===== aicommit summary ====="); n != 1 {
		t.Fatalf("summary printed %d times: %s", n, out.String())
	}
}

func TestInterruptWaitsForInFlightOperation(t *testing.T) {
	r := setup(t)
	a, _ := newApp(r, &fake{}, "", Options{Yes: true})
	locked, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		a.opMu.Lock()
		close(locked)
		<-release
		a.opMu.Unlock()
	}()
	<-locked
	go func() {
		a.Interrupt()
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("Interrupt returned while a git operation was in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Interrupt did not return after the operation finished")
	}
}

func TestInterruptStopsLaterGroups(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	a, _ := newApp(r, &fake{responses: []string{twoGroups}}, "", Options{Yes: true})
	a.Interrupt()
	if err := a.Run(context.Background()); !errors.Is(err, ErrQuit) {
		t.Fatalf("want ErrQuit, got %v", err)
	}
	if s := staged(t, r); len(s) != 0 {
		t.Fatalf("staged after interrupt: %v", s)
	}
	if got := log(t, r); got != nil {
		t.Fatalf("committed after interrupt: %v", got)
	}
}

func TestInterruptMidSplitRun(t *testing.T) {
	r := setup(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "b.txt", "b\n")
	write(t, r, "c.txt", "c\n")
	marker, release := filepath.Join(t.TempDir(), "marker"), filepath.Join(t.TempDir(), "release")
	hook(t, r, "post-commit", `if [ ! -f "`+marker+`" ]; then touch "`+marker+`"; while [ ! -f "`+release+`" ]; do sleep 0.01; done; fi`)
	three := `{"commits":[{"files":["a.txt"],"message":"feat: add a"},{"files":["b.txt"],"message":"feat: add b"},{"files":["c.txt"],"message":"feat: add c"}]}`
	a, _ := newApp(r, &fake{responses: []string{three}}, "", Options{Yes: true})
	a.Out = &lockedWriter{w: io.Discard}
	atInterrupt := make(chan int, 1)
	go func() {
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		go func() {
			// Interrupt blocks on the in-flight commit; long enough to be queued on opMu.
			time.Sleep(100 * time.Millisecond)
			os.WriteFile(release, nil, 0o644)
		}()
		a.Interrupt()
		atInterrupt <- len(log(t, r))
	}()
	err := a.Run(context.Background())
	n := <-atInterrupt
	if got := log(t, r); len(got) != n {
		t.Fatalf("commits after interrupt: had %d at interrupt, now %v", n, got)
	}
	if !errors.Is(err, ErrQuit) {
		t.Fatalf("want ErrQuit, got %v", err)
	}
	if s := staged(t, r); len(s) != 0 {
		t.Fatalf("staged after interrupt: %v", s)
	}
}

// lockedWriter serializes writes from the run and the interrupt goroutine.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
