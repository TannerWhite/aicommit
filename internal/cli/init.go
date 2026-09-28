package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/TannerWhite/aicommit/internal/config"
	"github.com/TannerWhite/aicommit/internal/rules"
)

const starterConfig = `# aicommit user config. Precedence: flags > AICOMMIT_* env > repo .aicommit/config.toml > this file.
# Repo configs may NOT set backend, command, base_url, or api_key_env.

# Leave backend unset to auto-detect: copilot CLI, then GitHub Models via "gh auth login".
# backend = "command"
# command = ["copilot", "-s", "--no-ask-user", "--model", "claude-sonnet-5"]

# backend = "openai"
# base_url = "http://localhost:11434/v1"   # Ollama; default is GitHub Models
# model = "llama3.1"
# api_key_env = "OPENAI_API_KEY"

# rules = "rules.md"            # relative to this directory
# max_file_bytes = 100000
# max_total_bytes = 400000
# timeout = "120s"
# exclude = ["*.sql"]           # added to the built-in secret patterns
`

func newInit(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Write a starter config.toml and rules.md to the user config directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.UserDir(os.Getenv)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			files := map[string]string{"config.toml": starterConfig, "rules.md": rules.Default()}
			for name := range files {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					return fmt.Errorf("%s already exists; not overwriting", filepath.Join(dir, name))
				}
			}
			for name, body := range files {
				p := filepath.Join(dir, name)
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					return err
				}
				fmt.Fprintln(stdout, "wrote", p)
			}
			return nil
		},
	}
}
