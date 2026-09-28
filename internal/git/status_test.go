package git

import (
	"reflect"
	"testing"
)

func TestParseStatusV2(t *testing.T) {
	in := "1 .M N... 100644 100644 100644 aaa bbb dir/a b.go\x00" +
		"2 R. N... 100644 100644 100644 aaa bbb R100 new.go\x00old.go\x00" +
		"u UU N... 100644 100644 100644 100644 a b c conflict.go\x00" +
		"? héllo.txt\x00"
	got, err := ParseStatusV2(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []StatusEntry{
		{Code: ".M", Path: "dir/a b.go"},
		{Code: "R.", Path: "new.go", OrigPath: "old.go"},
		{Code: "UU", Path: "conflict.go", Unmerged: true},
		{Code: "??", Path: "héllo.txt", Untracked: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestParseStatusV2Ignored(t *testing.T) {
	got, err := ParseStatusV2("! build/\x00")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestParseNameStatusZ(t *testing.T) {
	in := "M\x00a.go\x00R087\x00old.go\x00new.go\x00D\x00gone.go\x00"
	got, err := ParseNameStatusZ(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []StatusEntry{
		{Code: "M", Path: "a.go"},
		{Code: "R", Path: "new.go", OrigPath: "old.go"},
		{Code: "D", Path: "gone.go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}
