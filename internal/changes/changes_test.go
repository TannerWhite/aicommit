package changes

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/TannerWhite/aicommit/internal/git"
)

func repo(t *testing.T) git.Runner {
	t.Helper()
	r := git.Runner{Dir: t.TempDir()}
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := r.Run(nil, a...); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func write(t *testing.T, r git.Runner, name, body string) {
	t.Helper()
	p := filepath.Join(r.Dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func status(t *testing.T, r git.Runner) []git.StatusEntry {
	out, err := r.Run(nil, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		t.Fatal(err)
	}
	st, err := git.ParseStatusV2(out)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func opts() Options {
	return Options{MaxFileBytes: 100000, MaxTotalBytes: 400000, Exclude: DefaultExcludes}
}

func TestExcluded(t *testing.T) {
	cases := map[string]bool{".env": true, "app/.env.local": true, "certs/a.pem": true, "id_rsa.pub": true, "main.go": false, "env.go": false}
	for p, want := range cases {
		if got := Excluded(p, DefaultExcludes); got != want {
			t.Errorf("%s: got %v", p, got)
		}
	}
}

func TestBuildUntrackedSecretsBinaryAndSpaces(t *testing.T) {
	r := repo(t)
	write(t, r, "dir/a b.txt", "hello\n")
	write(t, r, ".env", "TOKEN=secret\n")
	write(t, r, "img.bin", "\x00\x01\x02")
	set, err := Build(r, status(t, r), opts())
	if err != nil {
		t.Fatal(err)
	}
	out := set.Render()
	if strings.Contains(out, "secret") {
		t.Fatal("secret leaked into render")
	}
	for _, want := range []string{"=== FILE: dir/a b.txt", "hello", "=== FILE: .env", "(contents excluded)", "(binary file, contents omitted)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestBuildRenameAndTruncation(t *testing.T) {
	r := repo(t)
	write(t, r, "old.txt", strings.Repeat("line\n", 50))
	r.Add([]string{"."})
	r.Commit("chore: init\n", false)
	r.Run(nil, "mv", "old.txt", "new.txt")
	write(t, r, "big.txt", strings.Repeat("x", 500))
	o := opts()
	o.MaxFileBytes = 100
	set, err := Build(r, status(t, r), o)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := set.Find("new.txt")
	if !ok || e.OrigPath != "old.txt" {
		t.Fatalf("rename not tracked: %+v", set.Entries)
	}
	b, _ := set.Find("big.txt")
	if !strings.Contains(b.Body, "[truncated: 400 bytes omitted]") {
		t.Fatalf("not truncated: %q", b.Body)
	}
}

func TestBuildTotalCap(t *testing.T) {
	r := repo(t)
	write(t, r, "a.txt", strings.Repeat("a", 300))
	write(t, r, "b.txt", strings.Repeat("b", 50))
	o := opts()
	o.MaxTotalBytes = 200
	set, _ := Build(r, status(t, r), o)
	a, _ := set.Find("a.txt")
	if !strings.HasPrefix(a.Body, "[omitted: 300 bytes, over total size limit]") {
		t.Fatalf("largest entry not summarized: %q", a.Body)
	}
	b, _ := set.Find("b.txt")
	if !strings.Contains(b.Body, "bbbb") {
		t.Fatal("small entry should be kept")
	}
}

func must(t *testing.T, r git.Runner, args ...string) string {
	t.Helper()
	out, err := r.Run(nil, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func TestBuildExcludedRenameSource(t *testing.T) {
	r := repo(t)
	base := "TOKEN=firstsecret\n" + strings.Repeat("KEY=value\n", 20)
	write(t, r, ".env", base)
	must(t, r, "add", ".env")
	must(t, r, "commit", "-q", "-m", "init")
	must(t, r, "mv", ".env", "config.txt")
	write(t, r, "config.txt", base+"OTHER=secondsecret\n")
	must(t, r, "add", "config.txt")
	st, err := git.ParseNameStatusZ(must(t, r, "diff", "--cached", "-z", "--name-status", "-M"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 1 || st[0].OrigPath != ".env" {
		t.Fatalf("expected a rename from .env: %+v", st)
	}
	o := opts()
	o.Staged = true
	set, err := Build(r, st, o)
	if err != nil {
		t.Fatal(err)
	}
	out := set.Render()
	if strings.Contains(out, "firstsecret") || strings.Contains(out, "secondsecret") {
		t.Fatalf("secret leaked:\n%s", out)
	}
	if !strings.Contains(out, "(contents excluded)") {
		t.Fatalf("missing excluded marker:\n%s", out)
	}
}

func TestBuildUntrackedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	r := repo(t)
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("TOKEN=linksecret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(r.Dir, "link")); err != nil {
		t.Fatal(err)
	}
	set, err := Build(r, status(t, r), opts())
	if err != nil {
		t.Fatal(err)
	}
	out := set.Render()
	if strings.Contains(out, "linksecret") {
		t.Fatalf("symlink target contents leaked:\n%s", out)
	}
	if !strings.Contains(out, target) {
		t.Fatalf("missing link target path:\n%s", out)
	}
}

func TestBuildBinaryDetection(t *testing.T) {
	r := repo(t)
	write(t, r, "notes.txt", "start\n")
	write(t, r, "data.bin", "\x00\x01\x02")
	must(t, r, "add", ".")
	must(t, r, "commit", "-q", "-m", "init")
	write(t, r, "notes.txt", "start\nBinary files a and b differ\n")
	write(t, r, "data.bin", "\x00\x03\x04\x05")
	set, err := Build(r, status(t, r), opts())
	if err != nil {
		t.Fatal(err)
	}
	n, ok := set.Find("notes.txt")
	if !ok || n.Binary || !strings.Contains(n.Body, "+Binary files a and b differ") {
		t.Fatalf("text file misdetected: %+v", n)
	}
	d, ok := set.Find("data.bin")
	if !ok || !d.Binary || d.Body != "" {
		t.Fatalf("binary file not detected: %+v", d)
	}
}

func TestBuildTreatsPathsLiterally(t *testing.T) {
	r := repo(t)
	write(t, r, "app/[id]/page.tsx", "one\n")
	write(t, r, "app/i/page.tsx", "two\n")
	must(t, r, "add", "-A")
	must(t, r, "commit", "-q", "-m", "init")
	write(t, r, "app/[id]/page.tsx", "one changed\n")
	write(t, r, "app/i/page.tsx", "two changed\n")
	for _, staged := range []bool{false, true} {
		if staged {
			must(t, r, "add", "-A")
		}
		var st []git.StatusEntry
		for _, e := range status(t, r) {
			if e.Path == "app/[id]/page.tsx" {
				st = append(st, e)
			}
		}
		o := opts()
		o.Staged = staged
		set, err := Build(r, st, o)
		if err != nil {
			t.Fatal(err)
		}
		e, ok := set.Find("app/[id]/page.tsx")
		if !ok || !strings.Contains(e.Body, "one changed") || strings.Contains(e.Body, "two") {
			t.Fatalf("staged=%v body:\n%s", staged, e.Body)
		}
	}
}
