package gitsrc

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateRepoURL checks a repository address before it is stored.
//
// Two separate problems live here. The address ends up inside a shell script in
// the build pod, so anything that can end a quoted string can run a command;
// and the build attaches the team's Git token to it, so an address pointing at
// somebody else's host is a way to post that token to them. Both are refused
// at the door rather than defended against later.
func ValidateRepoURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("a repository address is needed")
	}
	if len(raw) > 2048 {
		return "", fmt.Errorf("that repository address is too long")
	}
	for _, r := range raw {
		// Anything outside printable ASCII, and the space, cannot appear in a
		// URL that a shell will also see unchanged.
		if r <= ' ' || r > '~' {
			return "", fmt.Errorf("a repository address cannot contain spaces or control characters")
		}
	}
	// Checked before parsing: url.Parse percent-encodes some of these, and an
	// address that needs escaping to be safe is not an address anybody meant
	// to type. ? and # are left out, because a query and a fragment are
	// ordinary and are dropped below.
	if strings.ContainsAny(raw, `"'\`+"`"+`$;&|<>(){}*`) {
		return "", fmt.Errorf("a repository address cannot contain quotes or shell characters")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("that is not a valid repository address")
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("a repository address has to start with https://")
	}
	if parsed.User != nil {
		// A token in the URL would be stored in plain text and printed in
		// every build log. The Git connection is where credentials belong.
		return "", fmt.Errorf("leave the username and password out of the address; connect a Git account instead")
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("that repository address has no host")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return "", fmt.Errorf("that address has no repository in it")
	}
	// A query or a fragment is never part of a clone URL and would only be
	// carried into the build for no reason.
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String(), nil
}

// SameHost reports whether a repository is on the host a Git connection was
// made for.
//
// The build only attaches a connection's token when this is true. Without the
// check, pointing an app at a host of your choosing while selecting the team's
// GitHub connection sends that token straight to it.
func SameHost(repoURL, baseURL string) bool {
	repo, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil {
		return false
	}
	base, err := url.Parse(strings.TrimSpace(defaultBaseURL(baseURL)))
	if err != nil {
		return false
	}
	repoHost, baseHost := strings.ToLower(repo.Hostname()), strings.ToLower(base.Hostname())
	return repoHost != "" && repoHost == baseHost
}

// defaultBaseURL fills in github.com for a connection that never needed a base
// address, which is every GitHub one.
func defaultBaseURL(baseURL string) string {
	if strings.TrimSpace(baseURL) == "" {
		return "https://github.com"
	}
	if !strings.Contains(baseURL, "://") {
		return "https://" + baseURL
	}
	return baseURL
}
