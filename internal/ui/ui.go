// Package ui reads interactive choices and feedback lines from the user.
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Prompter reads user input from in and writes prompts to out.
type Prompter struct {
	in  *bufio.Reader
	out io.Writer
}

// New returns a Prompter reading from in and writing to out.
func New(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{in: bufio.NewReader(in), out: out}
}

// Choose prompts until the user enters a single letter from valid
// (case-insensitive). It returns io.EOF or the read error when input ends.
func (p *Prompter) Choose(prompt, valid string) (rune, error) {
	for {
		fmt.Fprint(p.out, prompt)
		line, err := p.in.ReadString('\n')
		s := strings.ToLower(strings.TrimSpace(line))
		if len(s) == 1 && strings.ContainsRune(valid, rune(s[0])) {
			return rune(s[0]), nil
		}
		if err != nil {
			fmt.Fprintln(p.out)
			return 0, err
		}
		fmt.Fprintf(p.out, "Please enter one of: %s\n", strings.Join(strings.Split(valid, ""), ", "))
	}
}

// Line prompts and returns one trimmed line of input.
func (p *Prompter) Line(prompt string) (string, error) {
	fmt.Fprint(p.out, prompt)
	line, err := p.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// IsTerminal reports whether f is a character device (an interactive terminal).
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
