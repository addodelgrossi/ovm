package auth

import (
	"errors"
	"net/url"
	"sort"
	"strings"
)

var errInvalidDigestDomain = errors.New("invalid HTTP Digest domain directive")

// protectionSpaceSet is the canonical set of request-target prefixes protected
// by one Digest challenge or Basic realm within the transport's exact origin.
// An origin-wide set is used only when a Digest challenge omits domain.
type protectionSpaceSet struct {
	originWide bool
	prefixes   []string
}

func originWideProtectionSpace() protectionSpaceSet {
	return protectionSpaceSet{originWide: true}
}

func (p protectionSpaceSet) key() string {
	if p.originWide {
		return "*"
	}
	return strings.Join(p.prefixes, "\x00")
}

func (p protectionSpaceSet) contains(requestTarget string) bool {
	if p.originWide {
		return true
	}
	for _, prefix := range p.prefixes {
		if strings.HasPrefix(requestTarget, prefix) {
			return true
		}
	}
	return false
}

func (p protectionSpaceSet) specificity(requestTarget string) int {
	if p.originWide {
		return 0
	}
	best := -1
	for _, prefix := range p.prefixes {
		if strings.HasPrefix(requestTarget, prefix) && len(prefix) > best {
			best = len(prefix)
		}
	}
	return best
}

// parseDomainDirective validates an RFC 7616 domain directive. The caller must
// invoke it only when the directive is present; omission is represented by
// originWideProtectionSpace. Every listed URI must be a same-origin HTTP(S)
// absolute URI or an absolute-path reference. A malformed or unsafe member
// rejects the directive instead of silently dropping it and broadening the
// challenge to the whole origin.
func parseDomainDirective(raw string, scope origin) (protectionSpaceSet, error) {
	entries := strings.Fields(raw)
	if len(entries) == 0 {
		return protectionSpaceSet{}, errInvalidDigestDomain
	}
	seen := make(map[string]struct{}, len(entries))
	prefixes := make([]string, 0, len(entries))
	for _, entry := range entries {
		prefix, ok := canonicalDomainEntry(entry, scope)
		if !ok {
			return protectionSpaceSet{}, errInvalidDigestDomain
		}
		if _, duplicate := seen[prefix]; duplicate {
			continue
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	if len(prefixes) == 0 {
		return protectionSpaceSet{}, errInvalidDigestDomain
	}
	sort.Strings(prefixes)
	return protectionSpaceSet{prefixes: prefixes}, nil
}

// canonicalDomainEntry returns the same-origin request-target prefix represented
// by one domain member. Query components are retained because RFC 7616 defines
// the protection space using URI-prefix matching, not path-directory matching.
func canonicalDomainEntry(entry string, scope origin) (string, bool) {
	if entry == "" || strings.HasPrefix(entry, "//") {
		return "", false
	}
	parsed, err := url.Parse(entry)
	if err != nil || parsed.Fragment != "" || parsed.User != nil || parsed.Opaque != "" {
		return "", false
	}

	if parsed.Scheme == "" {
		if parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
			return "", false
		}
		return requestTarget(parsed), true
	}

	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" || originFromURL(parsed) != scope {
		return "", false
	}
	return requestTarget(parsed), true
}

// requestTarget returns the escaped path and optional query used for Digest URI
// prefix matching. It intentionally excludes scheme and authority because the
// transport has already enforced an exact origin.
func requestTarget(value *url.URL) string {
	path := value.EscapedPath()
	if path == "" {
		path = "/"
	}
	if value.ForceQuery || value.RawQuery != "" {
		path += "?" + value.RawQuery
	}
	return path
}

// basicProtectionPrefix derives the directory protection space defined by RFC
// 7617 for a Basic challenge. Digest domain entries do not use this widening.
func basicProtectionPrefix(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	if strings.HasSuffix(path, "/") {
		return path
	}
	separator := strings.LastIndexByte(path, '/')
	if separator < 0 {
		return "/"
	}
	return path[:separator+1]
}
