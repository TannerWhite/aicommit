package backend

import (
	"errors"
	"fmt"
	"strings"

	"github.com/TannerWhite/aicommit/internal/config"
)

const (
	GitHubModelsURL    = "https://models.github.ai/inference"
	DefaultGitHubModel = "openai/gpt-4o-mini"
)

var ErrNoBackend = errors.New(`no AI backend found. Set one up with any of:
  1. install the GitHub Copilot CLI ("copilot" on PATH)
  2. run "gh auth login" to use GitHub Models
  3. configure an OpenAI-compatible endpoint (backend = "openai", base_url, model, api_key_env)
Run "aicommit init" to create a starter config`)

type Env struct {
	LookPath func(string) (string, error)
	Getenv   func(string) string
	GHToken  func() (string, error)
}

func New(c config.Config, e Env) (Backend, error) {
	switch c.Backend {
	case "command":
		if len(c.Command) == 0 {
			return nil, errors.New(`backend "command" requires command = ["prog", "args..."] in your user config`)
		}
		return Command{Argv: c.Command}, nil
	case "openai":
		return newOpenAI(c, e)
	case "":
		if _, err := e.LookPath("copilot"); err == nil {
			argv := []string{"copilot", "-s", "--no-ask-user"}
			if c.Model != "" {
				argv = append(argv, "--model", c.Model)
			}
			return Command{Argv: argv}, nil
		}
		if tok, err := e.GHToken(); err == nil && tok != "" {
			return newOpenAI(c, e)
		}
		return nil, ErrNoBackend
	}
	return nil, fmt.Errorf("unknown backend %q (want command or openai)", c.Backend)
}

func newOpenAI(c config.Config, e Env) (Backend, error) {
	o := OpenAI{BaseURL: c.BaseURL, Model: c.Model}
	if o.BaseURL == "" {
		o.BaseURL = GitHubModelsURL
	}
	isGH := strings.TrimRight(o.BaseURL, "/") == GitHubModelsURL
	if o.Model == "" {
		if !isGH {
			return nil, errors.New(`backend "openai" requires model to be set`)
		}
		o.Model = DefaultGitHubModel
	}
	switch {
	case c.APIKeyEnv != "":
		o.APIKey = e.Getenv(c.APIKeyEnv)
		if o.APIKey == "" {
			return nil, fmt.Errorf("api_key_env %s is empty or unset", c.APIKeyEnv)
		}
	case isGH:
		tok, err := e.GHToken()
		if err != nil {
			return nil, fmt.Errorf("GitHub Models needs a gh login: %w", err)
		}
		o.APIKey = tok
	}
	return o, nil
}
