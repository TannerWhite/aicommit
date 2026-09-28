package backend

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Command struct{ Argv []string }

func (c Command) Name() string { return "command: " + c.Argv[0] }

func (c Command) Generate(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	// Do not let inherited pipes keep Run waiting after the child is killed.
	cmd.WaitDelay = time.Second
	var out, errb bytes.Buffer
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: %w", c.Name(), ctx.Err())
		}
		return "", fmt.Errorf("%s: %w\n%s", c.Name(), err, tail(errb.String(), 20))
	}
	return out.String(), nil
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
