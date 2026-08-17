// Package fetcher fetches workflow run data from the GitHub Actions API.
// It targets a specific repo and returns failed runs along with their log URLs.
package fetcher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	baseURL = "https://api.github.com"
	userAgent = "flakewatch/0.1.0"
)

// Run represents a single GitHub Actions workflow run.
type Run struct {
	ID  int64 `json:"id"`
	Name string `json:"name"`
	HeadBranch string `json:"head_branch"`
	HeadSHA string `json:"head_sha"`
	Status string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL string `json:"html_url"`
	LogsURL string `json:"logs_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	WorkflowID int64 `json:"workflow_id"`
	WorkflowName string `json:"workflow_name,omitempty"`
}

// runsResponse is the raw API response wrapper.
type runsResponse struct {
	TotalCount int `json:"total_count"`
	WorkflowRuns []Run `json:"workflow_runs"`
}

// Client wraps the GitHub API with auth and rate-limit handling.
type Client struct {
	token string
	owner string
	repo string
	http *http.Client
}

// NewClient creates a new fetcher client.
func NewClient(owner, repo, token string) *Client {
	return &Client{
		token: token,
		owner: owner,
		repo: repo,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchFailedRuns returns up to limit failed workflow runs for the repo.
func (c *Client) FetchFailedRuns(limit int) ([]Run, error) {
	url := fmt.Sprintf(
		"%s/repos/%s/%s/actions/runs?status=failure&per_page=%d",
		baseURL, c.owner, c.repo, limit,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("rate limited or unauthorized (status 403) -- set GITHUB_TOKEN")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var result runsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return result.WorkflowRuns, nil
}

// FetchLogsURL returns the URL for downloading logs of a specific run.
// The URL redirects to a zip archive - we return it for the Python extractor.
func (c *Client) FetchLogsURL(runID int64) (string, error) {
	url := fmt.Sprintf(
		"%s/repos/%s/%s/actions/runs/%d/logs",
		baseURL, c.owner, c.repo, runID,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	// GitHub returns a redirect - we want the final URL, not the content
	c.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { c.http.CheckRedirect = nil }()

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 302 {
		return resp.Header.Get("Location"), nil
	}

	return "", fmt.Errorf("expected redirect, got %d", resp.StatusCode)
}
