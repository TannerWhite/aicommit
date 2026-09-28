package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OpenAI struct {
	BaseURL, Model, APIKey string
	Client                 *http.Client
}

func (o OpenAI) Name() string { return fmt.Sprintf("openai: %s (%s)", o.BaseURL, o.Model) }

func (o OpenAI) Generate(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    o.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", o.redact(fmt.Errorf("%s: %w", o.Name(), err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var parsed struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
		Error struct{ Message string } `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode/100 != 2 {
		msg := parsed.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
			// Redact before truncating so a key straddling the cut cannot leak a prefix.
			if o.APIKey != "" {
				msg = strings.ReplaceAll(msg, o.APIKey, "[REDACTED]")
			}
			if len(msg) > maxRawBody {
				msg = msg[:maxRawBody] + "..."
			}
		}
		return "", o.redact(fmt.Errorf("%s: HTTP %d: %s", o.Name(), resp.StatusCode, msg))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("%s: response had no choices", o.Name())
	}
	return parsed.Choices[0].Message.Content, nil
}

// maxRawBody caps how much of a non-JSON error body is echoed into an error.
const maxRawBody = 512

// redact hides the API key in err's text (servers or proxies may echo the
// Authorization header) while keeping err unwrappable via errors.Is.
func (o OpenAI) redact(err error) error {
	if o.APIKey == "" || !strings.Contains(err.Error(), o.APIKey) {
		return err
	}
	return redactedError{err: err, key: o.APIKey}
}

type redactedError struct {
	err error
	key string
}

func (e redactedError) Error() string { return strings.ReplaceAll(e.err.Error(), e.key, "[REDACTED]") }
func (e redactedError) Unwrap() error { return e.err }
