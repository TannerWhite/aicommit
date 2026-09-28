package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type StatusEntry struct {
	Code      string
	Path      string
	OrigPath  string
	Untracked bool
	Unmerged  bool
}

type Runner struct{ Dir string }

func (r Runner) cmd(args ...string) *exec.Cmd {
	c := exec.Command("git", args...)
	c.Dir = r.Dir
	return c
}

func (r Runner) Run(stdin io.Reader, args ...string) (string, error) {
	c := r.cmd(args...)
	var out, errb bytes.Buffer
	c.Stdin, c.Stdout, c.Stderr = stdin, &out, &errb
	if err := c.Run(); err != nil {
		sub := args[0]
		if sub == "--literal-pathspecs" && len(args) > 1 {
			sub = args[1]
		}
		return out.String(), fmt.Errorf("git %s: %w: %s", sub, err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func (r Runner) RepoRoot() (string, error) {
	out, err := r.Run(nil, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

func (r Runner) hasHead() bool {
	_, err := r.Run(nil, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

func (r Runner) HasStaged() (bool, error) {
	err := r.cmd("diff", "--cached", "--quiet").Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

func (r Runner) StagedPaths() ([]string, error) {
	out, err := r.Run(nil, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// Add, RestoreStaged and the change-set diffs pass paths with
// --literal-pathspecs so names such as app/[id]/page.tsx match only themselves.
func (r Runner) Add(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := r.Run(nil, append([]string{"--literal-pathspecs", "add", "-A", "--"}, paths...)...)
	return err
}

func (r Runner) RestoreStaged(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if !r.hasHead() {
		_, err := r.Run(nil, append([]string{"--literal-pathspecs", "rm", "--cached", "-r", "-q", "--ignore-unmatch", "--"}, paths...)...)
		return err
	}
	_, err := r.Run(nil, append([]string{"--literal-pathspecs", "restore", "--staged", "--"}, paths...)...)
	return err
}

// RestoreAllStaged unstages everything; it relies on the ":/" pathspec magic
// and so must not run in literal-pathspec mode.
func (r Runner) RestoreAllStaged() error {
	if !r.hasHead() {
		_, err := r.Run(nil, "rm", "--cached", "-r", "-q", "--ignore-unmatch", ":/")
		return err
	}
	_, err := r.Run(nil, "restore", "--staged", ":/")
	return err
}

// Commit commits the index with message. When edit is true git opens $EDITOR
// attached to the process terminal.
func (r Runner) Commit(message string, edit bool) (string, string, error) {
	f, err := os.CreateTemp("", "aicommit-msg-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(message); err != nil {
		f.Close()
		return "", "", err
	}
	f.Close()
	args := []string{"commit", "-F", f.Name()}
	if edit {
		args = append(args, "-e")
	}
	c := r.cmd(args...)
	var buf bytes.Buffer
	if edit {
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	} else {
		c.Stdout, c.Stderr = &buf, &buf
	}
	if err := c.Run(); err != nil {
		return "", buf.String(), fmt.Errorf("git commit failed: %w", err)
	}
	sha, err := r.Run(nil, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(sha), buf.String(), err
}
