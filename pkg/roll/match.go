package roll

import (
	"path"
	"strings"
)

// Select filters targets by an Ansible-style host expression (the --client
// flag): a comma/colon-separated list of glob terms, '!' to exclude, and the
// 'all' keyword. Terms match a host's node name, client types, or cl_el group.
// An empty expression selects all targets.
func Select(targets []Target, expr string) []Target {
	if strings.TrimSpace(expr) == "" {
		return targets
	}

	return applyLimit(targets, expr)
}

// applyLimit evaluates a comma/colon-separated expression of glob terms, with
// '!' for exclusion and the 'all' keyword. Includes are unioned; exclusions are
// subtracted. If the expression contains only exclusions, the base is all
// targets (so `!buildoor-*` means "everything except buildoor").
func applyLimit(targets []Target, expr string) []Target {
	var includes, excludes []string

	for _, term := range splitTerms(expr) {
		switch {
		case term == "":
		case strings.HasPrefix(term, "!"):
			excludes = append(excludes, strings.TrimPrefix(term, "!"))
		default:
			includes = append(includes, term)
		}
	}

	base := selectBase(targets, includes)

	out := make([]Target, 0, len(base))

	for _, t := range base {
		if !matchesAny(t, excludes) {
			out = append(out, t)
		}
	}

	return out
}

func selectBase(targets []Target, includes []string) []Target {
	if len(includes) == 0 || containsFold(includes, "all") {
		return targets
	}

	out := make([]Target, 0, len(targets))

	for _, t := range targets {
		if matchesAny(t, includes) {
			out = append(out, t)
		}
	}

	return out
}

func matchesAny(t Target, terms []string) bool {
	for _, term := range terms {
		if matchTerm(t, term) {
			return true
		}
	}

	return false
}

// matchTerm reports whether a term (a case-insensitive glob) matches any of the
// target's tokens (node name, client types, and the cl_el group).
func matchTerm(t Target, term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}

	if term == "all" {
		return true
	}

	for _, tok := range t.tokens {
		if ok, err := path.Match(term, tok); err == nil && ok {
			return true
		}
	}

	return false
}

// splitTerms splits a limit expression on commas and colons.
func splitTerms(expr string) []string {
	parts := strings.FieldsFunc(expr, func(r rune) bool { return r == ',' || r == ':' })
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	return parts
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}

	return false
}
