package git

import (
	"fmt"
	"strings"
)

// ParseStatusV2 parses `git status --porcelain=v2 -z --untracked-files=all`.
func ParseStatusV2(z string) ([]StatusEntry, error) {
	var out []StatusEntry
	recs := strings.Split(z, "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1':
			f := strings.SplitN(rec, " ", 9)
			if len(f) != 9 {
				return nil, fmt.Errorf("bad status record %q", rec)
			}
			out = append(out, StatusEntry{Code: f[1], Path: f[8]})
		case '2':
			f := strings.SplitN(rec, " ", 10)
			if len(f) != 10 || i+1 >= len(recs) {
				return nil, fmt.Errorf("bad rename record %q", rec)
			}
			i++
			out = append(out, StatusEntry{Code: f[1], Path: f[9], OrigPath: recs[i]})
		case 'u':
			f := strings.SplitN(rec, " ", 11)
			if len(f) != 11 {
				return nil, fmt.Errorf("bad unmerged record %q", rec)
			}
			out = append(out, StatusEntry{Code: f[1], Path: f[10], Unmerged: true})
		case '?':
			out = append(out, StatusEntry{Code: "??", Path: rec[2:], Untracked: true})
		case '!', '#':
		default:
			return nil, fmt.Errorf("unknown status record %q", rec)
		}
	}
	return out, nil
}

// ParseNameStatusZ parses `git diff --cached -z --name-status`.
func ParseNameStatusZ(z string) ([]StatusEntry, error) {
	var out []StatusEntry
	f := strings.Split(strings.TrimSuffix(z, "\x00"), "\x00")
	for i := 0; i < len(f); i++ {
		if f[i] == "" {
			continue
		}
		code := f[i][:1]
		if code == "R" || code == "C" {
			if i+2 >= len(f) {
				return nil, fmt.Errorf("truncated rename in name-status")
			}
			out = append(out, StatusEntry{Code: code, OrigPath: f[i+1], Path: f[i+2]})
			i += 2
			continue
		}
		if i+1 >= len(f) {
			return nil, fmt.Errorf("truncated name-status")
		}
		out = append(out, StatusEntry{Code: code, Path: f[i+1]})
		i++
	}
	return out, nil
}
