package prompt

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		os.WriteFile(p, []byte(got), 0o644)
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s mismatch; run go test ./internal/prompt -update\n%s", name, got)
	}
}

func TestSingle(t *testing.T) {
	golden(t, "single.golden", Single("RULES", "=== FILE: a.go ===\n+x\n", "shorter please"))
	if strings.Contains(Single("R", "C", ""), "rejected") {
		t.Fatal("feedback section should be omitted when empty")
	}
}

func TestSplit(t *testing.T) {
	golden(t, "split.golden", Split("RULES", "=== FILE: a.go ===\n", "file b.go missing"))
}

func TestNoEmDash(t *testing.T) {
	for _, s := range []string{Single("r", "c", "f"), Split("r", "c", "e")} {
		if strings.Contains(s, "\u2014") {
			t.Fatal("em dash in prompt")
		}
	}
}
