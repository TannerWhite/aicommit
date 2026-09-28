package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestChoose(t *testing.T) {
	var out bytes.Buffer
	p := New(strings.NewReader("x\nA\n"), &out)
	c, err := p.Choose("pick: ", "aq")
	if err != nil || c != 'a' {
		t.Fatalf("got %q %v", c, err)
	}
	if !strings.Contains(out.String(), "Please enter one of: a, q") {
		t.Fatalf("no reprompt: %q", out.String())
	}
	if _, err := p.Choose("pick: ", "aq"); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestChooseCRLF(t *testing.T) {
	p := New(strings.NewReader("a\r\n"), io.Discard)
	if c, err := p.Choose("pick: ", "aq"); err != nil || c != 'a' {
		t.Fatalf("got %q %v", c, err)
	}
}

func TestLine(t *testing.T) {
	p := New(strings.NewReader("  make it shorter \n"), io.Discard)
	if s, _ := p.Line("fb: "); s != "make it shorter" {
		t.Fatalf("got %q", s)
	}
}
