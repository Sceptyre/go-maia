// Package github is a minimal GitHub REST client for maia's issue
// detection and draft-PR write-back. Auth follows the "gh CLI first"
// convention: `gh auth token`, then GH_TOKEN/GITHUB_TOKEN, then the
// `github_token` key in ~/.maia/config.json (including {cmd:...} passthroughs).
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sceptyre/maia/internal/config"
)

// Issue mirrors the subset of the GitHub issue object maia uses.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	State     string    `json:"state"`
	Comments  int       `json:"comments"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	// Pull requests are returned by the issues endpoint too; exclude them.
	PullRequest *json.RawMessage `json:"pull_request"`
}

// Auth describes a resolved credential.
type Auth struct {
	OK     bool   `json:"ok"`
	Source string `json:"source,omitempty"` // "gh-cli" | "env:GH_TOKEN" | "config"
	Login  string `json:"login,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ResolveToken finds a GitHub credential using the documented precedence.
func ResolveToken() (token, source string) {
	if gh, err := exec.LookPath("gh"); err == nil {
		out, err := exec.Command(gh, "auth", "token").Output()
		if err == nil {
			if tok := strings.TrimSpace(string(out)); tok != "" {
				return tok, "gh-cli"
			}
		}
	}
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := os.Getenv(k); v != "" {
			return v, "env:" + k
		}
	}
	if v := config.Get("github_token"); v != "" {
		return v, "config"
	}
	return "", ""
}

type Client struct {
	token string
	http  *http.Client
}

// New resolves a token and returns a client (token may be empty: Auth will
// report why).
func New() *Client {
	tok, _ := ResolveToken()
	return &Client{token: tok, http: &http.Client{Timeout: 30 * time.Second}}
}

// Auth reports whether a usable credential exists and who it belongs to.
func (c *Client) Auth() Auth {
	if c.token == "" {
		return Auth{Error: "no GitHub token — install gh CLI (gh auth login), set GH_TOKEN, or github_token in Settings"}
	}
	login, err := c.Whoami()
	if err != nil {
		return Auth{Error: err.Error()}
	}
	_, source := ResolveToken()
	return Auth{OK: true, Source: source, Login: login}
}

func (c *Client) do(method, apiPath string, body any) ([]byte, int, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, 0, err
		}
	}
	req, err := http.NewRequest(method, "https://api.github.com"+apiPath, &buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := readAll(resp)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		return data, resp.StatusCode, fmt.Errorf("github API %d: %s", resp.StatusCode, apiMessage(data))
	}
	return data, resp.StatusCode, nil
}

func readAll(resp *http.Response) ([]byte, error) {
	var b bytes.Buffer
	_, err := b.ReadFrom(resp.Body)
	return b.Bytes(), err
}

func apiMessage(data []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &m) == nil && m.Message != "" {
		return m.Message
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Whoami returns the login the token belongs to.
func (c *Client) Whoami() (string, error) {
	data, _, err := c.do("GET", "/user", nil)
	if err != nil {
		return "", err
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(data, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

// ListOpenIssues returns open issues that carry all of the given labels,
// excluding pull requests.
func (c *Client) ListOpenIssues(repo string, labels []string) ([]Issue, error) {
	q := url.Values{}
	q.Set("state", "open")
	q.Set("per_page", "100")
	if len(labels) > 0 {
		q.Set("labels", strings.Join(labels, ","))
	}
	data, _, err := c.do("GET", "/repos/"+repo+"/issues?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var all []Issue
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(all))
	for _, is := range all {
		if is.PullRequest == nil {
			out = append(out, is)
		}
	}
	return out, nil
}

// DraftPR is the write-back payload.
type DraftPR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

// CreateDraftPR opens a draft pull request for head → base.
func (c *Client) CreateDraftPR(repo, title, body, head, base string) (*DraftPR, error) {
	payload := map[string]any{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
		"draft": true,
	}
	data, _, err := c.do("POST", "/repos/"+repo+"/pulls", payload)
	if err != nil {
		return nil, err
	}
	var pr DraftPR
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}
