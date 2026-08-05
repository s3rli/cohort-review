// Copied from code-review-agent cmd/server/config.go (parsePRURL/parsePRRef) —
// keep parity for same-source convergence.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// PRRef identifies a Bitbucket pull request.
type PRRef struct {
	Workspace string
	Repo      string
	Number    int
}

func parsePRURL(prURL string) (workspace, repo, prID string, err error) {
	// Expected format: https://bitbucket.org/{workspace}/{repo}/pull-requests/{id}
	parts := strings.Split(strings.TrimPrefix(prURL, "https://bitbucket.org/"), "/")
	if len(parts) != 4 || parts[2] != "pull-requests" {
		return "", "", "", fmt.Errorf("invalid PR URL: %s (expected https://bitbucket.org/{workspace}/{repo}/pull-requests/{id})", prURL)
	}
	return parts[0], parts[1], parts[3], nil
}

func parsePRRef(prURL string) (PRRef, error) {
	workspace, repo, prID, err := parsePRURL(prURL)
	if err != nil {
		return PRRef{}, err
	}
	n, err := strconv.Atoi(prID)
	if err != nil {
		return PRRef{}, fmt.Errorf("invalid PR number %q: %w", prID, err)
	}
	return PRRef{Workspace: workspace, Repo: repo, Number: n}, nil
}
