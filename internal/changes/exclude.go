package changes

import (
	"bytes"
	"path"
)

var DefaultExcludes = []string{".env", ".env.*", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "*.p12", "*.pfx"}

func Excluded(p string, patterns []string) bool {
	base := path.Base(p)
	for _, pat := range patterns {
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
