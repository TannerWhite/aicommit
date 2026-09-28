package changes

import (
	"bytes"
	"path"
	"strings"
)

var DefaultExcludes = []string{".env", ".env.*", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "*.p12", "*.pfx"}

// Excluded reports whether p or its base name matches any pattern,
// ignoring case so .ENV or Server.PEM are caught too.
func Excluded(p string, patterns []string) bool {
	p = strings.ToLower(p)
	base := path.Base(p)
	for _, pat := range patterns {
		pat = strings.ToLower(pat)
		if ok, _ := path.Match(pat, p); ok {
			return true
		}
		if ok, _ := path.Match(pat, base); ok {
			return true
		}
	}
	return false
}

func IsBinary(b []byte) bool {
	if len(b) > 8192 {
		b = b[:8192]
	}
	return bytes.IndexByte(b, 0) >= 0
}
