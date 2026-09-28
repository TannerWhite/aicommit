package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/TannerWhite/aicommit/internal/changes"
)

type Overrides struct {
	Backend, Model, Rules string
	Exclude               []string
}

type Config struct {
	Backend                         string
	Command                         []string
	BaseURL, APIKeyEnv, Model       string
	RulesFlag, RepoRules, UserRules string
	MaxFileBytes, MaxTotalBytes     int
	Timeout                         time.Duration
	Exclude                         []string
	Sources                         map[string]string
	Warnings                        []string
}

type file struct {
	Backend       *string  `toml:"backend"`
	Command       []string `toml:"command"`
	BaseURL       *string  `toml:"base_url"`
	APIKeyEnv     *string  `toml:"api_key_env"`
	Model         *string  `toml:"model"`
	Rules         *string  `toml:"rules"`
	MaxFileBytes  *int     `toml:"max_file_bytes"`
	MaxTotalBytes *int     `toml:"max_total_bytes"`
	Timeout       *string  `toml:"timeout"`
	Exclude       []string `toml:"exclude"`
}

func UserDir(getenv func(string) string) (string, error) {
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "aicommit"), nil
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, ".config", "aicommit"), nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "aicommit"), nil
}

func readFile(path string) (*file, toml.MetaData, error) {
	var f file
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, md, nil
	}
	if err != nil {
		return nil, md, fmt.Errorf("%s: %w", path, err)
	}
	return &f, md, nil
}

func Load(userDir, repoRoot string, getenv func(string) string, ov Overrides) (Config, error) {
	c := Config{
		MaxFileBytes:  100000,
		MaxTotalBytes: 400000,
		Timeout:       120 * time.Second,
		Exclude:       append([]string{}, changes.DefaultExcludes...),
		Sources: map[string]string{
			"max_file_bytes": "default", "max_total_bytes": "default", "timeout": "default", "exclude": "default",
		},
	}
	layers := []struct {
		name, path, base string
	}{
		{"user", filepath.Join(userDir, "config.toml"), userDir},
		{"repo", filepath.Join(repoRoot, ".aicommit", "config.toml"), repoRoot},
	}
	for _, l := range layers {
		if l.base == "" {
			continue
		}
		f, md, err := readFile(l.path)
		if err != nil {
			return Config{}, err
		}
		if f == nil {
			continue
		}
		for _, k := range md.Undecoded() {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s: unknown key %q", l.path, k.String()))
		}
		if l.name == "repo" {
			// Check the decoded struct, not md.IsDefined: the TOML decoder
			// matches keys case-insensitively, so "Command" still sets Command.
			for k, set := range map[string]bool{
				"backend":     f.Backend != nil,
				"command":     f.Command != nil,
				"base_url":    f.BaseURL != nil,
				"api_key_env": f.APIKeyEnv != nil,
			} {
				if set {
					return Config{}, fmt.Errorf("%s: %q is not allowed in repo config (it could run commands or send data elsewhere); set it in your user config instead", l.path, k)
				}
			}
		}
		if err := c.apply(f, l.name, l.base); err != nil {
			return Config{}, fmt.Errorf("%s: %w", l.path, err)
		}
	}
	envs := map[string]*string{
		"backend": &c.Backend, "model": &c.Model, "base_url": &c.BaseURL, "api_key_env": &c.APIKeyEnv, "rules": &c.RulesFlag,
	}
	for k, dst := range envs {
		if v := getenv("AICOMMIT_" + strings.ToUpper(k)); v != "" {
			*dst = v
			c.Sources[k] = "env"
		}
	}
	if v := getenv("AICOMMIT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("AICOMMIT_TIMEOUT: %w", err)
		}
		c.Timeout, c.Sources["timeout"] = d, "env"
	}
	set := func(k string, dst *string, v string) {
		if v != "" {
			*dst, c.Sources[k] = v, "flag"
		}
	}
	set("backend", &c.Backend, ov.Backend)
	set("model", &c.Model, ov.Model)
	set("rules", &c.RulesFlag, ov.Rules)
	if len(ov.Exclude) > 0 {
		c.Exclude = append(c.Exclude, ov.Exclude...)
		c.Sources["exclude"] = "flag"
	}
	return c, nil
}

func (c *Config) apply(f *file, src, base string) error {
	str := func(k string, dst *string, v *string) {
		if v != nil {
			*dst, c.Sources[k] = *v, src
		}
	}
	str("backend", &c.Backend, f.Backend)
	str("base_url", &c.BaseURL, f.BaseURL)
	str("api_key_env", &c.APIKeyEnv, f.APIKeyEnv)
	str("model", &c.Model, f.Model)
	if f.Command != nil {
		c.Command, c.Sources["command"] = f.Command, src
	}
	if f.Rules != nil {
		p := *f.Rules
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		if src == "repo" {
			c.RepoRules = p
		} else {
			c.UserRules = p
		}
		c.Sources["rules"] = src
	}
	if f.MaxFileBytes != nil {
		c.MaxFileBytes, c.Sources["max_file_bytes"] = *f.MaxFileBytes, src
	}
	if f.MaxTotalBytes != nil {
		c.MaxTotalBytes, c.Sources["max_total_bytes"] = *f.MaxTotalBytes, src
	}
	if f.Timeout != nil {
		d, err := time.ParseDuration(*f.Timeout)
		if err != nil {
			return fmt.Errorf("timeout: %w", err)
		}
		c.Timeout, c.Sources["timeout"] = d, src
	}
	if len(f.Exclude) > 0 {
		c.Exclude = append(c.Exclude, f.Exclude...)
		c.Sources["exclude"] = src
	}
	return nil
}

func (c Config) Describe() string {
	vals := map[string]string{
		"backend": c.Backend, "command": strings.Join(c.Command, " "), "base_url": c.BaseURL,
		"api_key_env": c.APIKeyEnv, "model": c.Model,
		"rules":          firstNonEmpty(c.RulesFlag, c.RepoRules, c.UserRules),
		"max_file_bytes": fmt.Sprint(c.MaxFileBytes), "max_total_bytes": fmt.Sprint(c.MaxTotalBytes),
		"timeout": c.Timeout.String(), "exclude": strings.Join(c.Exclude, ", "),
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		src := c.Sources[k]
		if src == "" {
			src = "unset"
		}
		fmt.Fprintf(&b, "%-16s = %-40s (%s)\n", k, vals[k], src)
	}
	return b.String()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
