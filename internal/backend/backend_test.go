package backend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"```\nfeat: x\n```":   "feat: x",
		"\"fix: y\"\n":        "fix: y",
		"  chore: z\n\nbody ": "chore: z\n\nbody",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q", in, got)
		}
	}
}

// TestHelperProcess is re-executed as a fake CLI by the command tests.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("AICOMMIT_HELPER") {
	case "echo":
		b, _ := io.ReadAll(os.Stdin)
		os.Stdout.WriteString("feat: " + strings.TrimSpace(string(b)))
		os.Exit(0)
	case "fail":
		os.Stderr.WriteString("boom\n")
		os.Exit(3)
	case "hang":
		time.Sleep(time.Minute)
	}
}

func helper(t *testing.T, mode string) Command {
	t.Setenv("AICOMMIT_HELPER", mode)
	return Command{Argv: []string{os.Args[0], "-test.run=^TestHelperProcess$"}}
}

func TestCommandBackend(t *testing.T) {
	out, err := helper(t, "echo").Generate(context.Background(), "hello")
	if err != nil || out != "feat: hello" {
		t.Fatalf("got %q %v", out, err)
	}
	_, err = helper(t, "fail").Generate(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err = helper(t, "hang").Generate(ctx, "x"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("sh"); err == nil {
			// argv is not shell-interpreted: "$HOME" must arrive literally.
			out, _ := Command{Argv: []string{"sh", "-c", "cat; printf '%s' \"$1\"", "sh", "$HOME"}}.Generate(context.Background(), "")
			if out != "$HOME" {
				t.Fatalf("argv was shell-expanded: %q", out)
			}
		}
	}
}

func TestOpenAIBackend(t *testing.T) {
	const key = "sk-secret-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, `{"error":{"message":"bad auth"}}`, http.StatusUnauthorized)
			return
		}
		var req struct {
			Model    string                     `json:"model"`
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Messages[0].Content == "slow" {
			time.Sleep(time.Second)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "feat: " + req.Model}}}})
	}))
	defer srv.Close()

	b := OpenAI{BaseURL: srv.URL, Model: "m1", APIKey: key}
	if out, err := b.Generate(context.Background(), "hi"); err != nil || out != "feat: m1" {
		t.Fatalf("got %q %v", out, err)
	}
	bad := OpenAI{BaseURL: srv.URL, Model: "m1", APIKey: "wrong-key-456"}
	_, err := bad.Generate(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad auth") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "wrong-key-456") || strings.Contains(b.Name(), key) {
		t.Fatal("key leaked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.Generate(ctx, "slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestOpenAIRedactsKeyFromEchoedBody(t *testing.T) {
	const key = "sk-echo-789"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		io.WriteString(w, "proxy error, got header: "+r.Header.Get("Authorization"))
	}))
	defer srv.Close()
	_, err := OpenAI{BaseURL: srv.URL, Model: "m1", APIKey: key}.Generate(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "[REDACTED]") || strings.Contains(err.Error(), key) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenAICapsRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		io.WriteString(w, strings.Repeat("x", 2000))
	}))
	defer srv.Close()
	_, err := OpenAI{BaseURL: srv.URL, Model: "m1"}.Generate(context.Background(), "hi")
	if err == nil || strings.Count(err.Error(), "x") > 512+1 || !strings.HasSuffix(err.Error(), "...") {
		t.Fatalf("got %d chars: %.80q", len(err.Error()), err)
	}
}
