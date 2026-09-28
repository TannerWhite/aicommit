package rules

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed default.md
var builtin string

func Default() string { return builtin }

type Sources struct{ Flag, RepoKey, UserKey, RepoRoot, UserDir string }

func Resolve(s Sources) (string, string, error) {
	type cand struct {
		path     string
		explicit bool
	}
	var cands []cand
	add := func(p string, explicit bool) {
		if p != "" {
			cands = append(cands, cand{p, explicit})
		}
	}
	add(s.Flag, true)
	add(s.RepoKey, true)
	if s.RepoRoot != "" {
		add(filepath.Join(s.RepoRoot, ".aicommit", "rules.md"), false)
	}
	add(s.UserKey, true)
	if s.UserDir != "" {
		add(filepath.Join(s.UserDir, "rules.md"), false)
	}
	for _, c := range cands {
		b, err := os.ReadFile(c.path)
		if err == nil {
			return string(b), c.path, nil
		}
		if c.explicit || !errors.Is(err, fs.ErrNotExist) {
			return "", "", fmt.Errorf("rules file %s: %w", c.path, err)
		}
	}
	return builtin, "built-in", nil
}
