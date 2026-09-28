package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Commit struct {
	Files   []string `json:"files"`
	Message string   `json:"message"`
}

type Plan struct {
	Commits []Commit `json:"commits"`
}

func decode(s string) (Plan, error) {
	var p Plan
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Plan{}, err
	}
	if dec.More() {
		return Plan{}, errors.New("trailing data after JSON object")
	}
	return p, nil
}

func Parse(raw string) (Plan, error) {
	p, err := decode(strings.TrimSpace(raw))
	if err == nil {
		return p, nil
	}
	obj, ok := firstObject(raw)
	if !ok {
		return Plan{}, fmt.Errorf("response is not JSON: %w", err)
	}
	return decode(obj)
}

// firstObject returns the first balanced top-level {...}, ignoring braces in strings.
func firstObject(s string) (string, bool) {
	start, depth, inStr, esc := -1, 0, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			if start >= 0 {
				inStr = true
			}
		case '{':
			if start < 0 {
				start = i
			}
			depth++
		case '}':
			if start >= 0 {
				depth--
				if depth == 0 {
					return s[start : i+1], true
				}
			}
		}
	}
	return "", false
}

func Validate(p Plan, paths []string) []string {
	var errs []string
	if len(p.Commits) == 0 {
		return []string{"plan has no commits"}
	}
	known := map[string]bool{}
	for _, x := range paths {
		known[x] = true
	}
	seen := map[string]int{}
	for i, c := range p.Commits {
		n := i + 1
		if strings.TrimSpace(c.Message) == "" {
			errs = append(errs, fmt.Sprintf("commit %d has an empty message", n))
		} else if strings.TrimSpace(strings.SplitN(c.Message, "\n", 2)[0]) == "" {
			errs = append(errs, fmt.Sprintf("commit %d has an empty subject line", n))
		}
		if len(c.Files) == 0 {
			errs = append(errs, fmt.Sprintf("commit %d has no files", n))
		}
		for _, f := range c.Files {
			if !known[f] {
				errs = append(errs, fmt.Sprintf("%q is not in the change set", f))
				continue
			}
			seen[f]++
			if seen[f] == 2 {
				errs = append(errs, fmt.Sprintf("%q appears in more than one commit", f))
			}
		}
	}
	for _, x := range paths {
		if seen[x] == 0 {
			errs = append(errs, fmt.Sprintf("%q is missing from the plan", x))
		}
	}
	return errs
}
