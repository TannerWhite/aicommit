package changes

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TannerWhite/aicommit/internal/git"
)

type Options struct {
	Staged        bool
	MaxFileBytes  int
	MaxTotalBytes int
	Exclude       []string
}

type Entry struct {
	Path, OrigPath, Status string
	Untracked, Excluded    bool
	Binary                 bool
	Body                   string
}

type Set struct{ Entries []Entry }

func Build(r git.Runner, st []git.StatusEntry, o Options) (Set, error) {
	var s Set
	for _, se := range st {
		e := Entry{Path: se.Path, OrigPath: se.OrigPath, Status: se.Code, Untracked: se.Untracked}
		e.Excluded = Excluded(se.Path, o.Exclude) || (se.OrigPath != "" && Excluded(se.OrigPath, o.Exclude))
		if !e.Excluded {
			body, bin, err := body(r, se, o.Staged)
			if err != nil {
				return Set{}, err
			}
			e.Binary = bin
			if !bin {
				e.Body = truncate(body, o.MaxFileBytes)
			}
		}
		s.Entries = append(s.Entries, e)
	}
	capTotal(&s, o.MaxTotalBytes)
	return s, nil
}

func body(r git.Runner, se git.StatusEntry, staged bool) (string, bool, error) {
	if se.Untracked {
		p := filepath.Join(r.Dir, filepath.FromSlash(se.Path))
		fi, err := os.Lstat(p)
		if err != nil {
			return "", false, err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t, err := os.Readlink(p)
			if err != nil {
				return "", false, err
			}
			return t, false, nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", false, err
		}
		return string(b), IsBinary(b), nil
	}
	paths := []string{se.Path}
	if se.OrigPath != "" {
		paths = []string{se.OrigPath, se.Path}
	}
	out, err := r.Run(nil, append([]string{"--literal-pathspecs", "diff", "--cached", "-M", "--"}, paths...)...)
	if err != nil {
		return "", false, err
	}
	if !staged {
		wt, err := r.Run(nil, append([]string{"--literal-pathspecs", "diff", "--"}, paths...)...)
		if err != nil {
			return "", false, err
		}
		out += wt
	}
	return out, gitBinary(out), nil
}

// gitBinary reports whether a diff contains git's own binary markers. Diff
// content lines start with '+', '-' or ' ', so they never match.
func gitBinary(diff string) bool {
	for _, l := range strings.Split(diff, "\n") {
		if l == "GIT binary patch" || (strings.HasPrefix(l, "Binary files ") && strings.HasSuffix(l, " differ")) {
			return true
		}
	}
	return false
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n[truncated: %d bytes omitted]\n", len(s)-max)
}

func capTotal(s *Set, max int) {
	total := 0
	for _, e := range s.Entries {
		total += len(e.Body)
	}
	idx := make([]int, len(s.Entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return len(s.Entries[idx[a]].Body) > len(s.Entries[idx[b]].Body) })
	for _, i := range idx {
		if max <= 0 || total <= max {
			return
		}
		e := &s.Entries[i]
		n := len(e.Body)
		e.Body = fmt.Sprintf("[omitted: %d bytes, over total size limit]\n", n)
		total += len(e.Body) - n
	}
}

func (s Set) Paths() []string {
	p := make([]string, len(s.Entries))
	for i, e := range s.Entries {
		p[i] = e.Path
	}
	return p
}

func (s Set) Find(p string) (Entry, bool) {
	for _, e := range s.Entries {
		if e.Path == p {
			return e, true
		}
	}
	return Entry{}, false
}

func (s Set) Render() string {
	var b strings.Builder
	for _, e := range s.Entries {
		fmt.Fprintf(&b, "\n=== FILE: %s (%s)", e.Path, describe(e))
		if e.OrigPath != "" {
			fmt.Fprintf(&b, " renamed from %s", e.OrigPath)
		}
		b.WriteString(" ===\n")
		switch {
		case e.Excluded:
			b.WriteString("(contents excluded)\n")
		case e.Binary:
			b.WriteString("(binary file, contents omitted)\n")
		default:
			b.WriteString(e.Body)
		}
	}
	return b.String()
}

func describe(e Entry) string {
	if e.Untracked {
		return "new file"
	}
	switch {
	case strings.Contains(e.Status, "D"):
		return "deleted"
	case strings.Contains(e.Status, "R"):
		return "renamed"
	case strings.Contains(e.Status, "A"):
		return "added"
	}
	return "modified"
}
