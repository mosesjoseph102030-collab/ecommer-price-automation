package slug

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	validSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	reserved  = map[string]bool{
		"admin": true, "api": true, "app": true, "login": true,
		"signup": true, "signin": true, "pricing": true, "support": true,
		"community": true, "www": true, "static": true, "assets": true,
	}
)

// Generate converts a store name to a URL slug: lowercase, numbers, hyphens only.
func Generate(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevHyphen := true // avoid leading hyphen
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r == ' ' || r == '_' || r == '-':
			if !prevHyphen && b.Len() > 0 {
				b.WriteRune('-')
				prevHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	return out
}

// Validate rejects empty, overlong, badly formed, or reserved slugs.
func Validate(s string) error {
	if s == "" {
		return errf("slug is required")
	}
	if len(s) < 3 || len(s) > 60 {
		return errf("slug must be 3-60 characters")
	}
	if !validSlug.MatchString(s) {
		return errf("slug must be lowercase letters, numbers, hyphens only")
	}
	if reserved[s] {
		return errf("slug %q is reserved", s)
	}
	return nil
}

type slugError string

func errf(format string, args ...any) slugError {
	return slugError(fmt.Sprintf(format, args...))
}

func (e slugError) Error() string { return string(e) }
