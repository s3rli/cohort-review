// Adapted from code-review-agent internal/vcs/bitbucket (manager.go apiRequest
// + vcs.go FetchPR/decodePR/FetchDiff/requireOK) — keep parity for same-source
// convergence. Collapsed Manager+BitbucketVCS into one read-only Client; no
// retry (a local CLI user just reruns); adds Title, Description, and the PR
// web link to the decode, which upstream doesn't extract.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBaseURL = "https://api.bitbucket.org"

var (
	errNotFound     = errors.New("not found")
	errUnauthorized = errors.New("unauthorized")
)

type Client struct {
	user, password string
	baseURL        string
	http           *http.Client
}

// NewClient constructs a Client. baseURL overrides the Bitbucket API base for
// tests; empty means production.
func NewClient(user, password, baseURL string) *Client {
	if baseURL == "" {
		baseURL = apiBaseURL
	}
	return &Client{user: user, password: password, baseURL: baseURL, http: &http.Client{Timeout: 30 * time.Second}}
}

type PullRequest struct {
	Title       string
	Description string // raw markdown, may be empty
	WebURL      string
}

func (c *Client) FetchPR(ctx context.Context, ref PRRef) (PullRequest, error) {
	resp, err := c.get(ctx, fmt.Sprintf("/2.0/repositories/%s/%s/pullrequests/%d", ref.Workspace, ref.Repo, ref.Number))
	if err != nil {
		return PullRequest{}, fmt.Errorf("fetch PR: %w", err)
	}
	defer resp.Body.Close()
	if err := requireOK(resp, "fetch PR"); err != nil {
		return PullRequest{}, err
	}
	var pr struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Links       struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return PullRequest{}, fmt.Errorf("decode PR: %w", err)
	}
	return PullRequest{Title: pr.Title, Description: pr.Description, WebURL: pr.Links.HTML.Href}, nil
}

// FetchDiff returns the source-vs-destination unified diff for a PR.
func (c *Client) FetchDiff(ctx context.Context, ref PRRef) (string, error) {
	resp, err := c.get(ctx, fmt.Sprintf("/2.0/repositories/%s/%s/pullrequests/%d/diff", ref.Workspace, ref.Repo, ref.Number))
	if err != nil {
		return "", fmt.Errorf("fetch PR diff: %w", err)
	}
	defer resp.Body.Close()
	if err := requireOK(resp, "fetch PR diff"); err != nil {
		return "", err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read PR diff body: %w", err)
	}
	return string(body), nil
}

func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.password)
	return c.http.Do(req)
}

// requireOK wraps a non-2xx response into a useful error, preserving the body.
func requireOK(resp *http.Response, op string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w: %s", op, errNotFound, string(body))
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s: %w: %s", op, errUnauthorized, string(body))
	}
	return fmt.Errorf("%s: status %d: %s", op, resp.StatusCode, string(body))
}
