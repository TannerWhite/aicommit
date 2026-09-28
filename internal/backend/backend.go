package backend

import (
	"context"
	"strings"
)

type Backend interface {
	Generate(ctx context.Context, prompt string) (string, error)
	Name() string
}

func Clean(s string) string {
	var kept []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			continue
		}
		kept = append(kept, l)
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	for _, q := range []string{`"`, `'`} {
		if len(out) >= 2 && strings.HasPrefix(out, q) && strings.HasSuffix(out, q) {
			out = strings.TrimSpace(out[1 : len(out)-1])
			break
		}
	}
	return out
}
