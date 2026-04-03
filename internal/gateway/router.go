package gateway

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

type RouteDecision struct {
	RouteID        string
	PathRuleSetID  string
	PoolID         string
	RequireMapping bool
}

func DecideRoute(routes []RouteRuleConfig, realPath string) *RouteDecision {
	sorted := make([]RouteRuleConfig, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	extracted := extractPath(realPath)
	p := ensureLeadingSlash(normalizePath(extracted))

	for _, r := range sorted {
		if !r.Enabled {
			continue
		}
		if matchRoute(r.Match, p) {
			return &RouteDecision{
				RouteID:        r.ID,
				PathRuleSetID:  r.PathRuleSetID,
				PoolID:         r.PoolID,
				RequireMapping: r.RequireMapping,
			}
		}
	}
	return nil
}

func matchRoute(match RouteMatchConfig, p string) bool {
	for _, prefix := range match.RealPathPrefix {
		np := normalizePath(prefix)
		if hasPathPrefix(p, np) {
			return true
		}
	}
	for _, pattern := range match.RealPathRegex {
		if re, err := regexp.Compile(pattern); err == nil {
			if re.MatchString(p) {
				return true
			}
		}
	}
	return len(match.RealPathPrefix) == 0 && len(match.RealPathRegex) == 0
}

func ResolveObjectKey(pathOrURL string, ruleSet *PathRuleSetConfig, requireMapping bool) (string, bool) {
	mappedPath, ok := resolveMappedPath(pathOrURL, ruleSet, requireMapping)
	if !ok {
		return "", false
	}
	key := strings.TrimLeft(mappedPath, "/")
	if key == "" {
		return "", false
	}
	return key, true
}

func resolveMappedPath(pathOrURL string, ruleSet *PathRuleSetConfig, requireMapping bool) (string, bool) {
	src := strings.TrimSpace(pathOrURL)
	if src == "" {
		return "", false
	}

	extracted := extractPath(src)
	extracted = normalizePath(extracted)

	if ruleSet == nil {
		p := ensureLeadingSlash(extracted)
		if p == "/" {
			return "", false
		}
		return p, true
	}

	mappedPath, matched := applyPathMappings(extracted, ruleSet.Mappings)
	if requireMapping && len(ruleSet.Mappings) > 0 && !matched {
		return "", false
	}
	if !matched {
		mappedPath = extracted
	}
	mappedPath = normalizePath(mappedPath)
	mappedPath = ensureLeadingSlash(mappedPath)
	if mappedPath == "/" {
		return "", false
	}
	return mappedPath, true
}

func extractPath(s string) string {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return s
		}
		p, err := url.PathUnescape(u.EscapedPath())
		if err != nil {
			return u.Path
		}
		return ensureLeadingSlash(p)
	}
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	return s
}

func normalizePath(p string) string {
	s := strings.TrimSpace(p)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\\", "/")
	cleaned := path.Clean(s)
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

func ensureLeadingSlash(s string) string {
	if s == "" {
		return "/"
	}
	if strings.HasPrefix(s, "/") {
		return s
	}
	return "/" + s
}

func hasPathPrefix(p string, prefix string) bool {
	if prefix == "" {
		return false
	}
	if prefix == "/" {
		return true
	}
	if !strings.HasPrefix(p, prefix) {
		return false
	}
	if len(p) == len(prefix) {
		return true
	}
	return p[len(prefix)] == '/'
}

func applyPathMappings(input string, mappings []PathMapping) (string, bool) {
	if len(mappings) == 0 {
		return input, false
	}
	in := normalizePath(input)
	for _, m := range mappings {
		from := normalizePath(m.From)
		if from == "" {
			continue
		}
		to := normalizePath(m.To)
		if to == "" {
			to = "/"
		}
		if !hasPathPrefix(in, from) {
			continue
		}
		rest := strings.TrimPrefix(in, from)
		if rest == "" {
			return to, true
		}
		if to == "/" {
			return rest, true
		}
		return strings.TrimRight(to, "/") + ensureLeadingSlash(rest), true
	}
	return input, false
}
