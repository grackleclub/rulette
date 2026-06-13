// Command triage walks unmoderated bug reports and rule suggestions one by
// one, letting an admin file the good ones as GitHub issues (via the gh CLI)
// or drop abusive ones. It talks to the running rulette server's admin
// endpoints, authenticating with the RULETTE_ADMIN_PASSWORD header.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/pterm/pterm"
)

const adminHeader = "X-Rulette-Admin"

type args struct {
	BaseURL string `arg:"--base-url,env:RULETTE_BASE_URL" default:"http://localhost:7777" help:"rulette server base URL"`
	Kind    string `arg:"--kind" default:"all" help:"which queue to triage: bugs, suggestions, or all"`
	Repo    string `arg:"--repo" default:"grackleclub/rulette" help:"GitHub repo for filed issues"`
}

// bug mirrors a row from GET /bugs.
type bug struct {
	ID          int32  `json:"id"`
	GameURL     string `json:"game_url"`
	OS          string `json:"os"`
	Browser     string `json:"browser"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Created     string `json:"created"`
}

// suggestion mirrors a row from GET /suggestions.
type suggestion struct {
	ID      int32  `json:"id"`
	Front   string `json:"front"`
	Back    string `json:"back"`
	Created string `json:"created"`
}

// patch is the status update body sent to PATCH /bugs/{id} or
// /suggestions/{id}.
type patch struct {
	Status   string `json:"status"`
	IssueURL string `json:"issue_url,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// client holds the config shared by every request.
type client struct {
	http     *http.Client
	baseURL  string
	password string
	repo     string
}

func main() {
	var a args
	arg.MustParse(&a)

	password := os.Getenv("RULETTE_ADMIN_PASSWORD")
	if password == "" {
		fatal("RULETTE_ADMIN_PASSWORD is not set")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		fatal("gh CLI not found on PATH: %v", err)
	}

	c := &client{
		http:     &http.Client{Timeout: 30 * time.Second},
		baseURL:  strings.TrimRight(a.BaseURL, "/"),
		password: password,
		repo:     a.Repo,
	}

	ctx := context.Background()
	switch a.Kind {
	case "bugs":
		mustRun(c.triageBugs(ctx))
	case "suggestions":
		mustRun(c.triageSuggestions(ctx))
	case "all":
		mustRun(c.triageBugs(ctx))
		mustRun(c.triageSuggestions(ctx))
	default:
		fatal("unknown --kind %q (want bugs, suggestions, or all)", a.Kind)
	}
}

// triageBugs lists new bug reports and walks each through a triage decision.
func (c *client) triageBugs(ctx context.Context) error {
	var bugs []bug
	if err := c.get(ctx, "/bugs", &bugs); err != nil {
		return fmt.Errorf("list bugs: %w", err)
	}
	if len(bugs) == 0 {
		pterm.Info.Println("no new bug reports")
		return nil
	}
	pterm.DefaultHeader.Printf("%d bug report(s)", len(bugs))
	for i, b := range bugs {
		pterm.DefaultBox.WithTitle(fmt.Sprintf("bug #%d (%d/%d)", b.ID, i+1, len(bugs))).
			Println(bugDetail(b))
		stop, err := c.decide(ctx, "bugs", b.ID, "bug",
			"[bug]: "+summary(b.Description), bugDetail(b))
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}
	return nil
}

// triageSuggestions lists new rule suggestions and walks each one.
func (c *client) triageSuggestions(ctx context.Context) error {
	var suggestions []suggestion
	if err := c.get(ctx, "/suggestions", &suggestions); err != nil {
		return fmt.Errorf("list suggestions: %w", err)
	}
	if len(suggestions) == 0 {
		pterm.Info.Println("no new rule suggestions")
		return nil
	}
	pterm.DefaultHeader.Printf("%d rule suggestion(s)", len(suggestions))
	for i, s := range suggestions {
		pterm.DefaultBox.WithTitle(fmt.Sprintf("suggestion #%d (%d/%d)", s.ID, i+1, len(suggestions))).
			Println(suggestionDetail(s))
		stop, err := c.decide(ctx, "suggestions", s.ID, "rule",
			"[rule]: "+s.Front, suggestionDetail(s))
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}
	return nil
}

// decide prompts for one ticket and carries out the choice. It returns
// stop=true when the operator chooses to quit. kind is the URL segment
// ("bugs"/"suggestions"); label, title, and detail seed the GitHub issue.
func (c *client) decide(ctx context.Context, kind string, id int32, label, title, detail string) (bool, error) {
	const (
		file   = "file GitHub issue"
		reject = "reject (keep, mark rejected)"
		del    = "delete (abusive)"
		skip   = "skip"
		quit   = "quit"
	)
	choice, err := pterm.DefaultInteractiveSelect.
		WithOptions([]string{file, reject, del, skip, quit}).Show()
	if err != nil {
		return false, fmt.Errorf("prompt: %w", err)
	}
	switch choice {
	case file:
		notes, _ := pterm.DefaultInteractiveTextInput.Show("developer notes (optional)")
		body := detail
		if strings.TrimSpace(notes) != "" {
			body += "\n\n---\n_developer notes: " + notes + "_"
		}
		url, err := ghIssue(ctx, c.repo, title, label, body)
		if err != nil {
			return false, fmt.Errorf("create issue: %w", err)
		}
		pterm.Success.Println("filed " + url)
		if err := c.patch(ctx, kind, id, patch{Status: "filed", IssueURL: url, Notes: notes}); err != nil {
			return false, fmt.Errorf("mark filed: %w", err)
		}
	case reject:
		notes, _ := pterm.DefaultInteractiveTextInput.Show("reason (optional)")
		if err := c.patch(ctx, kind, id, patch{Status: "rejected", Notes: notes}); err != nil {
			return false, fmt.Errorf("mark rejected: %w", err)
		}
		pterm.Info.Println("rejected")
	case del:
		ok, _ := pterm.DefaultInteractiveConfirm.Show("permanently delete this ticket?")
		if !ok {
			return false, nil
		}
		if err := c.delete(ctx, kind, id); err != nil {
			return false, fmt.Errorf("delete: %w", err)
		}
		pterm.Warning.Println("deleted")
	case skip:
		pterm.Info.Println("skipped")
	case quit:
		return true, nil
	}
	return false, nil
}

// ghIssue shells out to the gh CLI to open an issue and returns its URL,
// which gh prints on stdout.
func ghIssue(ctx context.Context, repo, title, label, body string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "issue", "create",
		"--repo", repo,
		"--title", title,
		"--label", label,
		"--body", body,
	)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh issue create: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func bugDetail(b bug) string {
	return fmt.Sprintf(
		"**Game URL:** %s\n**OS:** %s\n**Browser:** %s\n**Version:** %s\n\n### What happened\n%s",
		b.GameURL, b.OS, b.Browser, orNone(b.Version), b.Description,
	)
}

func suggestionDetail(s suggestion) string {
	return fmt.Sprintf("**Front:** %s\n**Back:** %s", s.Front, s.Back)
}

// summary returns the first line of s, trimmed to a reasonable issue-title
// length.
func summary(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// get fetches path and decodes the JSON response into out.
func (c *client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set(adminHeader, c.password)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return statusErr(res)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// patch updates a ticket's status.
func (c *client) patch(ctx context.Context, kind string, id int32, body patch) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode body: %w", err)
	}
	url := fmt.Sprintf("%s/%s?id=%d", c.baseURL, kind, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set(adminHeader, c.password)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		return statusErr(res)
	}
	return nil
}

// delete removes a ticket.
func (c *client) delete(ctx context.Context, kind string, id int32) error {
	url := fmt.Sprintf("%s/%s?id=%d", c.baseURL, kind, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set(adminHeader, c.password)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		return statusErr(res)
	}
	return nil
}

// statusErr turns a non-success response into an error carrying the body.
func statusErr(res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	return fmt.Errorf("unexpected status %s: %s", res.Status, strings.TrimSpace(string(body)))
}

func mustRun(err error) {
	if err != nil {
		fatal("%v", err)
	}
}

func fatal(format string, a ...any) {
	pterm.Error.Printfln(format, a...)
	os.Exit(1)
}
