package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func file(t *testing.T, p, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveOrder(t *testing.T) {
	repo, user := t.TempDir(), t.TempDir()
	s := Sources{RepoRoot: repo, UserDir: user}

	if text, src, _ := Resolve(s); src != "built-in" || !strings.Contains(text, "Conventional Commits") {
		t.Fatalf("expected built-in, got %s", src)
	}
	file(t, filepath.Join(user, "rules.md"), "user")
	if text, _, _ := Resolve(s); text != "user" {
		t.Fatal("user rules.md should win over built-in")
	}
	file(t, filepath.Join(repo, ".aicommit", "rules.md"), "repo")
	if text, _, _ := Resolve(s); text != "repo" {
		t.Fatal("repo rules.md should win over user")
	}
	s.Flag = file(t, filepath.Join(user, "flag.md"), "flag")
	if text, _, _ := Resolve(s); text != "flag" {
		t.Fatal("flag should win")
	}
}

func TestExplicitMissingIsError(t *testing.T) {
	_, _, err := Resolve(Sources{UserKey: "/nope/rules.md", RepoRoot: t.TempDir(), UserDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing explicit rules file")
	}
}

func TestDefaultHasNoEmDash(t *testing.T) {
	if strings.Contains(Default(), "\u2014") {
		t.Fatal("em dash in default rules")
	}
}
