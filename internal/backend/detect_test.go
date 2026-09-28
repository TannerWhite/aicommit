package backend

import (
	"errors"
	"strings"
	"testing"

	"github.com/TannerWhite/aicommit/internal/config"
)

func envWith(copilot bool, token string, vars map[string]string) Env {
	return Env{
		LookPath: func(n string) (string, error) {
			if copilot && n == "copilot" {
				return "/bin/copilot", nil
			}
			return "", errors.New("not found")
		},
		Getenv: func(k string) string { return vars[k] },
		GHToken: func() (string, error) {
			if token == "" {
				return "", errors.New("not logged in")
			}
			return token, nil
		},
	}
}

func TestAutoDetect(t *testing.T) {
	b, err := New(config.Config{Model: "m"}, envWith(true, "tok", nil))
	if c, ok := b.(Command); err != nil || !ok || strings.Join(c.Argv, " ") != "copilot -s --no-ask-user --model m" {
		t.Fatalf("want copilot, got %#v %v", b, err)
	}
	b, _ = New(config.Config{}, envWith(false, "tok", nil))
	if o, ok := b.(OpenAI); !ok || o.BaseURL != GitHubModelsURL || o.APIKey != "tok" || o.Model != DefaultGitHubModel {
		t.Fatalf("want github models, got %#v", b)
	}
	if _, err := New(config.Config{}, envWith(false, "", nil)); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("want ErrNoBackend, got %v", err)
	}
}

func TestExplicit(t *testing.T) {
	if _, err := New(config.Config{Backend: "command"}, envWith(false, "", nil)); err == nil {
		t.Fatal("command without argv must fail")
	}
	b, _ := New(config.Config{Backend: "openai", BaseURL: "http://localhost:11434/v1", Model: "llama3"}, envWith(false, "", nil))
	if o := b.(OpenAI); o.APIKey != "" || o.Model != "llama3" {
		t.Fatalf("got %#v", o)
	}
	if _, err := New(config.Config{Backend: "openai", BaseURL: "http://x", Model: "m", APIKeyEnv: "K"}, envWith(false, "", nil)); err == nil {
		t.Fatal("empty api key env must fail")
	}
	b, _ = New(config.Config{Backend: "openai", BaseURL: "http://x", Model: "m", APIKeyEnv: "K"}, envWith(false, "", map[string]string{"K": "v"}))
	if b.(OpenAI).APIKey != "v" {
		t.Fatal("key not read from env")
	}
	if _, err := New(config.Config{Backend: "nope"}, envWith(false, "", nil)); err == nil {
		t.Fatal("unknown backend must fail")
	}
}
