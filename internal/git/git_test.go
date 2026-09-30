package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRepo(t *testing.T) Runner {
	t.Helper()
	r := Runner{Dir: t.TempDir()}
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		if _, err := r.Run(nil, a...); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestAddCommitRestore(t *testing.T) {
	r := newRepo(t)
	os.WriteFile(filepath.Join(r.Dir, "a.txt"), []byte("a\n"), 0o644)
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.HasStaged(); !ok {
		t.Fatal("expected staged")
	}
	if err := r.RestoreStaged([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.HasStaged(); ok {
		t.Fatal("expected nothing staged after restore on unborn branch")
	}
	r.Add([]string{"a.txt"})
	sha, _, err := r.Commit("feat: add a\n", false)
	if err != nil || sha == "" {
		t.Fatalf("commit: %v %q", err, sha)
	}
	log, _ := r.Run(nil, "log", "--format=%s")
	if strings.TrimSpace(log) != "feat: add a" {
		t.Fatalf("log %q", log)
	}
}

func TestEmptyPathLists(t *testing.T) {
	r := newRepo(t)
	if err := os.WriteFile(filepath.Join(r.Dir, "u.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(nil); err != nil {
		t.Fatal(err)
	}
	ok, err := r.HasStaged()
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("Add(nil) staged files")
	}
	if err := r.RestoreStaged(nil); err != nil {
		t.Fatalf("RestoreStaged(nil): %v", err)
	}
}

func TestRestoreStagedWithHead(t *testing.T) {
	r := newRepo(t)
	p := filepath.Join(r.Dir, "a.txt")
	if err := os.WriteFile(p, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Commit("feat: a\n", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	ok, err := r.HasStaged()
	if err != nil || !ok {
		t.Fatalf("expected staged: %v %v", ok, err)
	}
	if err := r.RestoreStaged([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	ok, err = r.HasStaged()
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected nothing staged after restore")
	}
}

func TestAddTreatsPathsLiterally(t *testing.T) {
	r := newRepo(t)
	for _, p := range []string{"app/[id]/page.tsx", "app/i/page.tsx", "x[y].txt", "xy.txt"} {
		os.MkdirAll(filepath.Join(r.Dir, filepath.Dir(p)), 0o755)
		if err := os.WriteFile(filepath.Join(r.Dir, p), []byte(p+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, withHead := range []bool{false, true} {
		if withHead {
			if err := r.Add([]string{"xy.txt"}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := r.Commit("chore: init\n", false); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(r.Dir, "xy.txt"), []byte("changed\n"), 0o644)
		}
		if err := r.Add([]string{"app/[id]/page.tsx", "x[y].txt"}); err != nil {
			t.Fatal(err)
		}
		got, _ := r.StagedPaths()
		if strings.Join(got, ",") != "app/[id]/page.tsx,x[y].txt" {
			t.Fatalf("head=%v staged %v", withHead, got)
		}
		r.Add([]string{"app/i/page.tsx", "xy.txt"})
		if err := r.RestoreStaged([]string{"app/[id]/page.tsx", "x[y].txt"}); err != nil {
			t.Fatal(err)
		}
		got, _ = r.StagedPaths()
		if strings.Join(got, ",") != "app/i/page.tsx,xy.txt" {
			t.Fatalf("head=%v after restore %v", withHead, got)
		}
		if err := r.RestoreAllStaged(); err != nil {
			t.Fatal(err)
		}
		if got, _ = r.StagedPaths(); len(got) != 0 {
			t.Fatalf("head=%v after restore all %v", withHead, got)
		}
	}
}
