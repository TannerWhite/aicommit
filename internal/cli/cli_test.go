package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}} {
		c := exec.Command("git", a...)
		c.Dir = d
		if err := c.Run(); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func run(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)
	var out, errb bytes.Buffer
	code := Execute(args, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func TestInitAndConfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir := repoDir(t)
	if code, _, e := run(t, dir, "init"); code != 0 {
		t.Fatalf("init failed: %s", e)
	}
	for _, f := range []string{"config.toml", "rules.md"} {
		if _, err := os.Stat(filepath.Join(xdg, "aicommit", f)); err != nil {
			t.Fatal(err)
		}
	}
	if code, _, _ := run(t, dir, "init"); code != 1 {
		t.Fatal("init must refuse to overwrite")
	}
	code, out, _ := run(t, dir, "config", "--model", "m9")
	if code != 0 || !strings.Contains(out, "m9") || !strings.Contains(out, "(flag)") {
		t.Fatalf("config output: %s", out)
	}
}

func TestRepoConfigForbiddenKeyExits1(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := repoDir(t)
	os.MkdirAll(filepath.Join(dir, ".aicommit"), 0o755)
	os.WriteFile(filepath.Join(dir, ".aicommit", "config.toml"), []byte(`command = ["evil"]`), 0o644)
	code, _, e := run(t, dir, "config")
	if code != 1 || !strings.Contains(e, "not allowed in repo config") {
		t.Fatalf("code %d stderr %s", code, e)
	}
}

func TestCleanTreeExits0(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AICOMMIT_BACKEND", "command")
	dir := repoDir(t)
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "aicommit")
	os.MkdirAll(cfg, 0o755)
	os.WriteFile(filepath.Join(cfg, "config.toml"), []byte(`command = ["cat"]`), 0o644)
	code, out, e := run(t, dir, "--yes")
	if code != 0 || !strings.Contains(out, "nothing to commit") {
		t.Fatalf("code %d out %s err %s", code, out, e)
	}
}

func TestNoTTYExits2(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AICOMMIT_BACKEND", "command")
	dir := repoDir(t)
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "aicommit")
	os.MkdirAll(cfg, 0o755)
	os.WriteFile(filepath.Join(cfg, "config.toml"), []byte(`command = ["cat"]`), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	if code, _, _ := run(t, dir); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}
