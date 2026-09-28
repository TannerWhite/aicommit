package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestHelperInterrupt(t *testing.T) {
	if os.Getenv("AICOMMIT_HELPER_INTERRUPT") != "1" {
		t.Skip("helper")
	}
	os.Exit(Execute([]string{"--yes"}, strings.NewReader(""), os.Stdout, os.Stderr))
}

func TestInterruptExits2(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt cannot be sent on windows")
	}
	xdg := t.TempDir()
	dir := repoDir(t)
	cfg := filepath.Join(xdg, "aicommit")
	os.MkdirAll(cfg, 0o755)
	// The backend touches ready once it starts, which is after signal.Notify.
	ready := filepath.Join(t.TempDir(), "ready")
	os.WriteFile(filepath.Join(cfg, "config.toml"), []byte(fmt.Sprintf(`command = ["sh", "-c", %q]`, `touch "`+ready+`"; exec sleep 30`)), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	add := exec.Command("git", "add", "a.txt")
	add.Dir = dir
	if err := add.Run(); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(os.Args[0], "-test.run=^TestHelperInterrupt$")
	c.Dir = dir
	c.Env = append(os.Environ(), "AICOMMIT_HELPER_INTERRUPT=1", "XDG_CONFIG_HOME="+xdg, "AICOMMIT_BACKEND=command")
	var errb bytes.Buffer
	c.Stderr = &errb
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			c.Process.Kill()
			c.Wait()
			t.Fatalf("backend never started; stderr %s", errb.String())
		}
	}
	c.Process.Signal(os.Interrupt)
	c.Wait()
	e := errb.String()
	if code := c.ProcessState.ExitCode(); code != 2 || !strings.Contains(e, "aicommit: interrupted") || strings.Contains(e, "context canceled") {
		t.Fatalf("code %d stderr %s", code, e)
	}
}
