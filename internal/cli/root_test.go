package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var out, errb bytes.Buffer
	code := Execute([]string{"--version"}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	if !strings.Contains(out.String(), "aicommit dev") {
		t.Fatalf("got %q", out.String())
	}
}
