package plan

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	good := `{"commits":[{"files":["a.go"],"message":"feat: a"}]}`
	for _, in := range []string{good, "Sure! Here it is:\n" + good + "\nHope that helps {x}", "```json\n" + good + "\n```"} {
		p, err := Parse(in)
		if err != nil || len(p.Commits) != 1 || p.Commits[0].Files[0] != "a.go" {
			t.Fatalf("Parse(%q) = %+v, %v", in, p, err)
		}
	}
	if _, err := Parse(`{"commits":[{"files":["a"],"message":"m","extra":1}]}`); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
	if _, err := Parse("no json here"); err == nil {
		t.Fatal("expected error")
	}
	p, err := Parse(`note {"commits":[{"files":["a}b.go"],"message":"fix: brace \" }"}]}`)
	if err != nil || p.Commits[0].Files[0] != "a}b.go" {
		t.Fatalf("brace inside string broke extraction: %v", err)
	}
}

func TestValidate(t *testing.T) {
	paths := []string{"a.go", "b.go"}
	ok := Plan{Commits: []Commit{{Files: []string{"a.go"}, Message: "feat: a"}, {Files: []string{"b.go"}, Message: "fix: b"}}}
	if errs := Validate(ok, paths); len(errs) != 0 {
		t.Fatalf("unexpected %v", errs)
	}
	cases := map[string]Plan{
		`"b.go" is missing`:               {Commits: []Commit{{Files: []string{"a.go"}, Message: "feat: a"}}},
		`"a.go" appears in more than one`: {Commits: []Commit{{Files: []string{"a.go", "b.go"}, Message: "x: y"}, {Files: []string{"a.go"}, Message: "x: z"}}},
		`"c.go" is not in the change set`: {Commits: []Commit{{Files: []string{"a.go", "b.go", "c.go"}, Message: "x: y"}}},
		`commit 1 has an empty message`:   {Commits: []Commit{{Files: []string{"a.go", "b.go"}, Message: "  \n"}}},
		`commit 1 has no files`:           {Commits: []Commit{{Message: "x: y"}, {Files: paths, Message: "x: y"}}},
		`plan has no commits`:             {},
	}
	for want, p := range cases {
		errs := strings.Join(Validate(p, paths), "\n")
		if !strings.Contains(errs, want) {
			t.Errorf("want %q in %q", want, errs)
		}
	}
}
