package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gh "github.com/google/go-github/v60/github"
	"github.com/your-org/dashboard/internal/connector"
)

// Client wraps the go-github client and a plain HTTP client for GraphQL.
type Client struct {
	client *gh.Client
	token  string
}

// DraftIssue represents a draft issue in a GitHub ProjectV2.
type DraftIssue struct {
	Title string
	Body  string
}

// New creates a GitHub Client with token-authenticated transport.
// Returns a Client with empty token if token is ""; methods will return an error.
func New(token string) *Client {
	if token == "" {
		return &Client{}
	}
	c := gh.NewClient(nil).WithAuthToken(token)
	return &Client{client: c, token: token}
}

func (c *Client) checkToken() error {
	if c.token == "" || c.client == nil {
		return connector.NewErrCredentialsMissing("GITHUB_TOKEN")
	}
	return nil
}

// parseTarget splits "owner/repo" into (owner, repo).
func parseTarget(target string) (string, string, error) {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("github: target must be in 'owner/repo' format, got %q", target)
	}
	return parts[0], parts[1], nil
}

// graphql executes a GraphQL query against the GitHub API.
func (c *Client) graphql(ctx context.Context, query string, variables map[string]any, result any) error {
	body := map[string]any{"query": query, "variables": variables}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("github graphql marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("github graphql request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("github graphql do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github graphql read: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github graphql: status %d: %s", resp.StatusCode, respBody)
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("github graphql decode: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("github graphql error: %s", envelope.Errors[0].Message)
	}
	if result != nil && envelope.Data != nil {
		return json.Unmarshal(envelope.Data, result)
	}
	return nil
}

// Discover enumerates labels, GitHub ProjectsV2, and .md files in the target "owner/repo".
func (c *Client) Discover(ctx context.Context, target string) ([]connector.DiscoveredItem, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}
	owner, repo, err := parseTarget(target)
	if err != nil {
		return nil, err
	}

	var items []connector.DiscoveredItem

	// 1. Labels
	labelOpts := &gh.ListOptions{PerPage: 100}
	for {
		labels, resp, err := c.client.Issues.ListLabels(ctx, owner, repo, labelOpts)
		if err != nil {
			return nil, fmt.Errorf("github: list labels: %w", err)
		}
		for _, l := range labels {
			items = append(items, connector.DiscoveredItem{
				SourceType: "github_label",
				ExternalID: fmt.Sprintf("%s/%s/labels/%s", owner, repo, l.GetName()),
				Title:      l.GetName(),
				URL:        fmt.Sprintf("https://github.com/%s/%s/labels/%s", owner, repo, l.GetName()),
				SourceMeta: map[string]any{
					"owner": owner,
					"repo":  repo,
					"color": l.GetColor(),
				},
				Excerpt: l.GetDescription(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		labelOpts.Page = resp.NextPage
	}

	// 2. GitHub Projects (ProjectV2) via GraphQL
	projectItems, err := c.discoverProjects(ctx, owner, repo)
	if err != nil {
		// Non-fatal: projects may not be enabled
		_ = err
	} else {
		items = append(items, projectItems...)
	}

	// 3. Markdown files via git tree API
	mdItems, err := c.discoverMarkdownFiles(ctx, owner, repo)
	if err != nil {
		_ = err
	} else {
		items = append(items, mdItems...)
	}

	return items, nil
}

func (c *Client) discoverProjects(ctx context.Context, owner, repo string) ([]connector.DiscoveredItem, error) {
	type projectNode struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	type pageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	}
	type projectsConn struct {
		PageInfo pageInfo      `json:"pageInfo"`
		Nodes    []projectNode `json:"nodes"`
	}
	type repoData struct {
		Repository struct {
			ProjectsV2 projectsConn `json:"projectsV2"`
		} `json:"repository"`
	}

	const query = `query($owner: String!, $repo: String!, $cursor: String) {
		repository(owner: $owner, name: $repo) {
			projectsV2(first: 20, after: $cursor) {
				pageInfo { hasNextPage endCursor }
				nodes { id title url }
			}
		}
	}`

	var items []connector.DiscoveredItem
	var cursor *string
	for {
		vars := map[string]any{"owner": owner, "repo": repo, "cursor": cursor}
		var data repoData
		if err := c.graphql(ctx, query, vars, &data); err != nil {
			return nil, err
		}
		for _, p := range data.Repository.ProjectsV2.Nodes {
			items = append(items, connector.DiscoveredItem{
				SourceType: "github_project",
				ExternalID: p.ID,
				Title:      p.Title,
				URL:        p.URL,
				SourceMeta: map[string]any{
					"owner":      owner,
					"repo":       repo,
					"project_id": p.ID,
				},
			})
		}
		if !data.Repository.ProjectsV2.PageInfo.HasNextPage {
			break
		}
		c2 := data.Repository.ProjectsV2.PageInfo.EndCursor
		cursor = &c2
	}
	return items, nil
}

func (c *Client) discoverMarkdownFiles(ctx context.Context, owner, repo string) ([]connector.DiscoveredItem, error) {
	// Get default branch to resolve tree SHA
	repoInfo, _, err := c.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("github: get repo: %w", err)
	}
	defaultBranch := repoInfo.GetDefaultBranch()

	branch, _, err := c.client.Repositories.GetBranch(ctx, owner, repo, defaultBranch, 0)
	if err != nil {
		return nil, fmt.Errorf("github: get branch: %w", err)
	}
	treeSHA := branch.GetCommit().GetCommit().GetTree().GetSHA()

	tree, _, err := c.client.Git.GetTree(ctx, owner, repo, treeSHA, true)
	if err != nil {
		return nil, fmt.Errorf("github: get tree: %w", err)
	}

	var items []connector.DiscoveredItem
	for _, entry := range tree.Entries {
		if entry.GetType() != "blob" {
			continue
		}
		path := entry.GetPath()
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			continue
		}
		fileURL := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", owner, repo, defaultBranch, path)
		items = append(items, connector.DiscoveredItem{
			SourceType: "github_md_file",
			ExternalID: fmt.Sprintf("%s/%s/blob/%s", owner, repo, path),
			Title:      path,
			URL:        fileURL,
			SourceMeta: map[string]any{
				"owner": owner,
				"repo":  repo,
				"path":  path,
				"sha":   entry.GetSHA(),
			},
		})
	}
	return items, nil
}

// FetchIssues fetches issues matching the given label updated after since,
// using the GitHub search API.
func (c *Client) FetchIssues(ctx context.Context, owner, repo, label string, since time.Time) ([]*gh.Issue, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	sinceStr := since.UTC().Format("2006-01-02")
	query := fmt.Sprintf("repo:%s/%s label:%s updated:>%s", owner, repo, label, sinceStr)

	var all []*gh.Issue
	opts := &gh.SearchOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		result, resp, err := c.client.Search.Issues(ctx, query, opts)
		if err != nil {
			return nil, fmt.Errorf("github: search issues: %w", err)
		}
		all = append(all, result.Issues...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// FetchDraftIssues fetches draft issues from a ProjectV2, filtered by Team/Area field value.
func (c *Client) FetchDraftIssues(ctx context.Context, owner, repo, projectID, teamAreaValue string) ([]DraftIssue, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	type draftIssueContent struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	type fieldValue struct {
		Text string `json:"text"`
	}
	type projectItem struct {
		Type    string            `json:"type"`
		Content draftIssueContent `json:"content"`
		FieldValues struct {
			Nodes []struct {
				Text  string `json:"text"`
				Field struct {
					Name string `json:"name"`
				} `json:"field"`
			} `json:"nodes"`
		} `json:"fieldValues"`
	}
	type pageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	}
	type projectData struct {
		Node struct {
			Items struct {
				PageInfo pageInfo      `json:"pageInfo"`
				Nodes    []projectItem `json:"nodes"`
			} `json:"items"`
		} `json:"node"`
	}

	const query = `query($projectID: ID!, $cursor: String) {
		node(id: $projectID) {
			... on ProjectV2 {
				items(first: 50, after: $cursor) {
					pageInfo { hasNextPage endCursor }
					nodes {
						type
						content {
							... on DraftIssue {
								title
								body
							}
						}
						fieldValues(first: 20) {
							nodes {
								... on ProjectV2ItemFieldTextValue {
									text
									field { ... on ProjectV2Field { name } }
								}
								... on ProjectV2ItemFieldSingleSelectValue {
									name
									field { ... on ProjectV2SingleSelectField { name } }
								}
							}
						}
					}
				}
			}
		}
	}`

	var results []DraftIssue
	var cursor *string
	for {
		vars := map[string]any{"projectID": projectID, "cursor": cursor}
		var data projectData
		if err := c.graphql(ctx, query, vars, &data); err != nil {
			return nil, err
		}
		for _, item := range data.Node.Items.Nodes {
			if item.Type != "DRAFT_ISSUE" {
				continue
			}
			// Check Team/Area field value
			matched := teamAreaValue == ""
			for _, fv := range item.FieldValues.Nodes {
				fieldName := strings.ToLower(fv.Field.Name)
				if fieldName == "team" || fieldName == "area" || fieldName == "team/area" {
					if fv.Text == teamAreaValue {
						matched = true
						break
					}
				}
			}
			if matched {
				results = append(results, DraftIssue{
					Title: item.Content.Title,
					Body:  item.Content.Body,
				})
			}
		}
		if !data.Node.Items.PageInfo.HasNextPage {
			break
		}
		c2 := data.Node.Items.PageInfo.EndCursor
		cursor = &c2
	}
	return results, nil
}

// FetchMergedPRs lists closed PRs and returns those merged after since.
func (c *Client) FetchMergedPRs(ctx context.Context, owner, repo string, since time.Time) ([]*gh.PullRequest, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	opts := &gh.PullRequestListOptions{
		State:     "closed",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	var merged []*gh.PullRequest
	for {
		prs, resp, err := c.client.PullRequests.List(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("github: list pull requests: %w", err)
		}
		done := false
		for _, pr := range prs {
			if pr.MergedAt == nil {
				continue
			}
			if pr.MergedAt.Time.Before(since) {
				// Since list is sorted by updated desc, once we hit old entries we can stop
				done = true
				break
			}
			merged = append(merged, pr)
		}
		if done || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return merged, nil
}

// FetchCommits fetches commits between since and until, filtered client-side by author login.
func (c *Client) FetchCommits(ctx context.Context, owner, repo string, since, until time.Time, authors []string) ([]*gh.RepositoryCommit, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	authorSet := make(map[string]bool, len(authors))
	for _, a := range authors {
		authorSet[strings.ToLower(a)] = true
	}

	opts := &gh.CommitsListOptions{
		Since: since,
		Until: until,
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	var result []*gh.RepositoryCommit
	for {
		commits, resp, err := c.client.Repositories.ListCommits(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("github: list commits: %w", err)
		}
		for _, commit := range commits {
			if len(authorSet) == 0 {
				result = append(result, commit)
				continue
			}
			// Filter by author login
			login := ""
			if commit.Author != nil {
				login = strings.ToLower(commit.Author.GetLogin())
			}
			if authorSet[login] {
				result = append(result, commit)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return result, nil
}

// FetchMarkdownFile fetches the raw content and blob SHA of a file in a repository.
func (c *Client) FetchMarkdownFile(ctx context.Context, owner, repo, path string) (content string, sha string, err error) {
	if err := c.checkToken(); err != nil {
		return "", "", err
	}

	fileContent, _, _, err := c.client.Repositories.GetContents(ctx, owner, repo, path, nil)
	if err != nil {
		return "", "", fmt.Errorf("github: get contents %s: %w", path, err)
	}
	if fileContent == nil {
		return "", "", fmt.Errorf("github: %s is a directory, not a file", path)
	}

	sha = fileContent.GetSHA()
	// go-github's GetContent() decodes base64 automatically
	decoded, decodeErr := fileContent.GetContent()
	if decodeErr != nil {
		return "", "", fmt.Errorf("github: decode content %s: %w", path, decodeErr)
	}
	content = decoded
	return content, sha, nil
}

// AutoTagIssues pages all items in the project; for each item with Team/Area set
// but missing the corresponding label on the linked issue, applies the label.
func (c *Client) AutoTagIssues(ctx context.Context, owner, repo, projectID string, teamLabelMap map[string]string) error {
	if err := c.checkToken(); err != nil {
		return err
	}

	type issueContent struct {
		Number int    `json:"number"`
		Labels struct {
			Nodes []struct {
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"labels"`
	}
	type projectItem struct {
		Type    string       `json:"type"`
		Content issueContent `json:"content"`
		FieldValues struct {
			Nodes []struct {
				Name  string `json:"name"`
				Field struct {
					Name string `json:"name"`
				} `json:"field"`
			} `json:"nodes"`
		} `json:"fieldValues"`
	}
	type pageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	}
	type projectData struct {
		Node struct {
			Items struct {
				PageInfo pageInfo      `json:"pageInfo"`
				Nodes    []projectItem `json:"nodes"`
			} `json:"items"`
		} `json:"node"`
	}

	const query = `query($projectID: ID!, $cursor: String) {
		node(id: $projectID) {
			... on ProjectV2 {
				items(first: 50, after: $cursor) {
					pageInfo { hasNextPage endCursor }
					nodes {
						type
						content {
							... on Issue {
								number
								labels(first: 20) {
									nodes { name }
								}
							}
						}
						fieldValues(first: 20) {
							nodes {
								... on ProjectV2ItemFieldSingleSelectValue {
									name
									field { ... on ProjectV2SingleSelectField { name } }
								}
								... on ProjectV2ItemFieldTextValue {
									text
									field { ... on ProjectV2Field { name } }
								}
							}
						}
					}
				}
			}
		}
	}`

	var cursor *string
	for {
		vars := map[string]any{"projectID": projectID, "cursor": cursor}
		var data projectData
		if err := c.graphql(ctx, query, vars, &data); err != nil {
			return err
		}

		for _, item := range data.Node.Items.Nodes {
			if item.Type != "ISSUE" {
				continue
			}

			// Find Team/Area field value
			teamAreaValue := ""
			for _, fv := range item.FieldValues.Nodes {
				fn := strings.ToLower(fv.Field.Name)
				if fn == "team" || fn == "area" || fn == "team/area" {
					if fv.Name != "" {
						teamAreaValue = fv.Name
					}
					break
				}
			}
			if teamAreaValue == "" {
				continue
			}

			// Look up target label
			targetLabel, ok := teamLabelMap[teamAreaValue]
			if !ok {
				continue
			}

			// Check if issue already has the label
			hasLabel := false
			for _, lbl := range item.Content.Labels.Nodes {
				if lbl.Name == targetLabel {
					hasLabel = true
					break
				}
			}
			if hasLabel {
				continue
			}

			// Apply the label
			issueNum := item.Content.Number
			if issueNum == 0 {
				continue
			}
			_, _, err := c.client.Issues.AddLabelsToIssue(ctx, owner, repo, issueNum, []string{targetLabel})
			if err != nil {
				return fmt.Errorf("github: add label %q to issue #%d: %w", targetLabel, issueNum, err)
			}
		}

		if !data.Node.Items.PageInfo.HasNextPage {
			break
		}
		c2 := data.Node.Items.PageInfo.EndCursor
		cursor = &c2
	}
	return nil
}
