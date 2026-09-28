package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaults(t *testing.T) {
	c, err := Load(t.TempDir(), t.TempDir(), env(nil), Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxFileBytes != 100000 || c.MaxTotalBytes != 400000 || c.Timeout != 120*time.Second {
		t.Fatalf("bad defaults %+v", c)
	}
	if c.Sources["timeout"] != "default" || len(c.Exclude) != 8 {
		t.Fatalf("sources %+v excl %v", c.Sources, c.Exclude)
	}
}

func TestPrecedence(t *testing.T) {
	user, repo := t.TempDir(), t.TempDir()
	write(t, filepath.Join(user, "config.toml"), "backend = \"openai\"\nmodel = \"u\"\ntimeout = \"30s\"\nrules = \"mine.md\"\n")
	write(t, filepath.Join(repo, ".aicommit", "config.toml"), "model = \"r\"\nexclude = [\"*.sql\"]\nrules = \"docs/rules.md\"\n")
	c, err := Load(user, repo, env(map[string]string{"AICOMMIT_TIMEOUT": "10s"}), Overrides{Exclude: []string{"*.lock"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Backend != "openai" || c.Sources["backend"] != "user" {
		t.Fatal("backend should come from user")
	}
	if c.Model != "r" || c.Sources["model"] != "repo" {
		t.Fatal("repo model should beat user")
	}
	if c.Timeout != 10*time.Second || c.Sources["timeout"] != "env" {
		t.Fatal("env should beat files")
	}
	if c.UserRules != filepath.Join(user, "mine.md") || c.RepoRules != filepath.Join(repo, "docs", "rules.md") {
		t.Fatalf("rules paths %q %q", c.UserRules, c.RepoRules)
	}
	joined := strings.Join(c.Exclude, ",")
	if !strings.Contains(joined, "*.sql") || !strings.Contains(joined, "*.lock") || !strings.Contains(joined, ".env") {
		t.Fatalf("exclude %v", c.Exclude)
	}
	c2, err := Load(user, repo, env(nil), Overrides{Model: "f"})
	if err != nil {
		t.Fatal(err)
	}
	if c2.Model != "f" || c2.Sources["model"] != "flag" {
		t.Fatal("flag should win")
	}
}

func TestRepoCannotSetDangerousKeys(t *testing.T) {
	for _, key := range []string{`backend = "command"`, `command = ["sh", "-c", "evil"]`, `base_url = "http://evil"`, `api_key_env = "X"`} {
		repo := t.TempDir()
		write(t, filepath.Join(repo, ".aicommit", "config.toml"), key+"\n")
		_, err := Load(t.TempDir(), repo, env(nil), Overrides{})
		if err == nil || !strings.Contains(err.Error(), "not allowed in repo config") {
			t.Fatalf("%s: expected fail-closed error, got %v", key, err)
		}
	}
}

func TestUnknownKeyWarns(t *testing.T) {
	user := t.TempDir()
	write(t, filepath.Join(user, "config.toml"), "modle = \"x\"\n")
	c, err := Load(user, t.TempDir(), env(nil), Overrides{})
	if err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "modle") {
		t.Fatalf("warnings %v err %v", c.Warnings, err)
	}
}

func TestUserDirXDG(t *testing.T) {
	d, err := UserDir(env(map[string]string{"XDG_CONFIG_HOME": "/x"}))
	if err != nil {
		t.Fatal(err)
	}
	if d != filepath.Join("/x", "aicommit") {
		t.Fatalf("got %s", d)
	}
}

func TestRepoCannotSetDangerousKeysAnyCase(t *testing.T) {
	for _, key := range []string{`Command = ["sh", "-c", "evil"]`, `BASE_URL = "http://evil"`, `Backend = "command"`, `Api_Key_Env = "X"`} {
		repo := t.TempDir()
		write(t, filepath.Join(repo, ".aicommit", "config.toml"), key+"\n")
		_, err := Load(t.TempDir(), repo, env(nil), Overrides{})
		if err == nil || !strings.Contains(err.Error(), "not allowed in repo config") {
			t.Fatalf("%s: expected fail-closed error, got %v", key, err)
		}
	}
}
