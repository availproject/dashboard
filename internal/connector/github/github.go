package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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

// BoardItem represents a linked issue on a GitHub ProjectV2 board.
// State is normalized to lowercase ("open"/"closed").
type BoardItem struct {
	Number        int
	Title         string
	State         string   // "open" or "closed"
	NodeID        string
	Owner         string
	Repo          string
	Assignees     []string // GitHub logins
	Labels        []string // label names
	Status        string   // project Status field value, e.g. "In Progress"
	TeamArea      string   // Team/Area field value
	Sprint        string   // iteration title, e.g. "Sprint 12"
	SprintStart   string   // "YYYY-MM-DD"
	SprintDays    int      // iteration duration in days
}

// FetchProjectItems pages through all items on a GitHub ProjectV2 board and returns
// the linked issues as BoardItems. Draft issues and pull requests are skipped.
func (c *Client) FetchProjectItems(ctx context.Context, projectID string) ([]BoardItem, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	type issueContent struct {
		ID         string `json:"id"`
		Number     int    `json:"number"`
		Title      string `json:"title"`
		State      string `json:"state"`
		Repository struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
		Assignees struct {
			Nodes []struct {
				Login string `json:"login"`
			} `json:"nodes"`
		} `json:"assignees"`
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
				Name      string `json:"name"`      // single-select value
				Text      string `json:"text"`      // text field value
				Title     string `json:"title"`     // iteration title
				StartDate string `json:"startDate"` // iteration start date
				Duration  int    `json:"duration"`  // iteration duration in days
				Field     struct {
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
				items(first: 100, after: $cursor) {
					pageInfo { hasNextPage endCursor }
					nodes {
						type
						content {
							... on Issue {
								id
								number
								title
								state
								repository {
									name
									owner { login }
								}
								assignees(first: 10) {
									nodes { login }
								}
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
								... on ProjectV2ItemFieldIterationValue {
									title
									startDate
									duration
									field { ... on ProjectV2IterationField { name } }
								}
							}
						}
					}
				}
			}
		}
	}`

	var results []BoardItem
	var cursor *string
	for {
		vars := map[string]any{"projectID": projectID, "cursor": cursor}
		var data projectData
		if err := c.graphql(ctx, query, vars, &data); err != nil {
			return nil, err
		}

		for _, item := range data.Node.Items.Nodes {
			if item.Type != "ISSUE" {
				continue
			}
			content := item.Content
			if content.Number == 0 {
				continue
			}
			bi := BoardItem{
				NodeID: content.ID,
				Number: content.Number,
				Title:  content.Title,
				State:  strings.ToLower(content.State),
				Owner:  content.Repository.Owner.Login,
				Repo:   content.Repository.Name,
			}
			for _, a := range content.Assignees.Nodes {
				if a.Login != "" {
					bi.Assignees = append(bi.Assignees, a.Login)
				}
			}
			for _, l := range content.Labels.Nodes {
				if l.Name != "" {
					bi.Labels = append(bi.Labels, l.Name)
				}
			}
			for _, fv := range item.FieldValues.Nodes {
				fn := strings.ToLower(strings.ReplaceAll(fv.Field.Name, " ", ""))
				switch fn {
				case "status":
					if fv.Name != "" {
						bi.Status = fv.Name
					}
				case "team", "area", "team/area":
					val := fv.Name
					if val == "" {
						val = fv.Text
					}
					if val != "" {
						bi.TeamArea = val
					}
				default:
					// Capture any iteration field value (sprint).
					if fv.Title != "" && fv.StartDate != "" {
						bi.Sprint = fv.Title
						bi.SprintStart = fv.StartDate
						bi.SprintDays = fv.Duration
					}
				}
			}
			results = append(results, bi)
		}

		if !data.Node.Items.PageInfo.HasNextPage {
			break
		}
		c2 := data.Node.Items.PageInfo.EndCursor
		cursor = &c2
	}
	return results, nil
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

// Discover enumerates a github_repo root item and any GitHub ProjectsV2
// linked to it. Labels and markdown files are no longer discovered — label-based
// issue filtering was replaced by ProjectV2 board membership.
func (c *Client) Discover(ctx context.Context, target string) ([]connector.DiscoveredItem, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}
	owner, repo, err := parseTarget(target)
	if err != nil {
		return nil, err
	}

	repoExtID := owner + "/" + repo

	// Root: the repo itself.
	items := []connector.DiscoveredItem{{
		SourceType: "github_repo",
		ExternalID: repoExtID,
		Title:      repoExtID,
		URL:        fmt.Sprintf("https://github.com/%s/%s", owner, repo),
		SourceMeta: map[string]any{"owner": owner, "repo": repo},
	}}

	// GitHub Projects linked to this repo.
	projectItems, err := c.discoverProjects(ctx, owner, repo)
	if err == nil {
		for i := range projectItems {
			projectItems[i].ParentExternalID = repoExtID
			projectItems[i].ParentSourceType = "github_repo"
		}
		items = append(items, projectItems...)
	}

	return items, nil
}

// discoverLabels fetches all labels from owner/repo without setting parent info.
func (c *Client) discoverLabels(ctx context.Context, owner, repo string) ([]connector.DiscoveredItem, error) {
	var items []connector.DiscoveredItem
	opts := &gh.ListOptions{PerPage: 100}
	for {
		labels, resp, err := c.client.Issues.ListLabels(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("github: list labels: %w", err)
		}
		for _, l := range labels {
			items = append(items, connector.DiscoveredItem{
				SourceType: "github_label",
				ExternalID: fmt.Sprintf("%s/%s/labels/%s", owner, repo, l.GetName()),
				Title:      l.GetName(),
				URL:        fmt.Sprintf("https://github.com/%s/%s/labels/%s", owner, repo, l.GetName()),
				SourceMeta: map[string]any{"owner": owner, "repo": repo, "color": l.GetColor()},
				Excerpt:    l.GetDescription(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
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

// DiscoverProject discovers sources reachable from a GitHub ProjectV2.
// target must be in "org/project-number" format (e.g. "acme/5").
// It fetches the project's linked repositories via a single fast GraphQL query
// (no item pagination) then runs full label/file discovery on each repo.
func (c *Client) DiscoverProject(ctx context.Context, target string) ([]connector.DiscoveredItem, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	org, numberStr, found := strings.Cut(target, "/")
	if !found || org == "" || numberStr == "" {
		return nil, fmt.Errorf("github: project target must be 'org/project-number', got %q", target)
	}
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		return nil, fmt.Errorf("github: project number must be an integer, got %q", numberStr)
	}

	type repoNode struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	type projectData struct {
		Organization struct {
			ProjectV2 struct {
				ID           string `json:"id"`
				Title        string `json:"title"`
				URL          string `json:"url"`
				Repositories struct {
					Nodes []repoNode `json:"nodes"`
				} `json:"repositories"`
			} `json:"projectV2"`
		} `json:"organization"`
	}

	const query = `query($org: String!, $number: Int!) {
		organization(login: $org) {
			projectV2(number: $number) {
				id
				title
				url
				repositories(first: 20) {
					nodes {
						name
						owner { login }
					}
				}
			}
		}
	}`

	var data projectData
	if err := c.graphql(ctx, query, map[string]any{"org": org, "number": number}, &data); err != nil {
		return nil, fmt.Errorf("github: discover project: %w", err)
	}

	proj := data.Organization.ProjectV2
	if proj.ID == "" {
		return nil, fmt.Errorf("github: project %q not found in org %q", numberStr, org)
	}

	// Root: the project itself.
	items := []connector.DiscoveredItem{{
		SourceType: "github_project",
		ExternalID: proj.ID,
		Title:      proj.Title,
		URL:        proj.URL,
		SourceMeta: map[string]any{
			"org":            org,
			"project_number": number,
			"project_id":     proj.ID,
		},
	}}

	// Emit each linked repo as a child of the project.
	for _, repo := range proj.Repositories.Nodes {
		repoOwner := repo.Owner.Login
		repoName := repo.Name
		repoExtID := repoOwner + "/" + repoName

		items = append(items, connector.DiscoveredItem{
			SourceType:       "github_repo",
			ExternalID:       repoExtID,
			Title:            repoExtID,
			URL:              fmt.Sprintf("https://github.com/%s/%s", repoOwner, repoName),
			SourceMeta:       map[string]any{"owner": repoOwner, "repo": repoName},
			ParentExternalID: proj.ID,
			ParentSourceType: "github_project",
		})
	}

	return items, nil
}

// ProjectField describes a single field on a GitHub ProjectV2 board.
type ProjectField struct {
	Name    string   // field display name
	Type    string   // "single_select", "iteration", "text", "number", "date", "other"
	Options []string // single_select: option names; iteration: iteration titles (newest first)
}

// FetchProjectFields returns all fields defined on a GitHub ProjectV2 board,
// including the available options for single-select fields and iteration titles.
func (c *Client) FetchProjectFields(ctx context.Context, projectID string) ([]ProjectField, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	type singleSelectOption struct {
		Name string `json:"name"`
	}
	type iteration struct {
		Title string `json:"title"`
	}
	type rawField struct {
		// Common
		Name string `json:"name"`
		// ProjectV2SingleSelectField
		Options []singleSelectOption `json:"options"`
		// ProjectV2IterationField
		Configuration struct {
			Iterations []iteration `json:"iterations"`
		} `json:"configuration"`
	}
	type fieldsData struct {
		Node struct {
			Fields struct {
				Nodes []json.RawMessage `json:"nodes"`
			} `json:"fields"`
		} `json:"node"`
	}

	const query = `query($projectID: ID!) {
		node(id: $projectID) {
			... on ProjectV2 {
				fields(first: 50) {
					nodes {
						... on ProjectV2Field {
							name
						}
						... on ProjectV2SingleSelectField {
							name
							options { name }
						}
						... on ProjectV2IterationField {
							name
							configuration {
								iterations { title }
							}
						}
					}
				}
			}
		}
	}`

	var data fieldsData
	if err := c.graphql(ctx, query, map[string]any{"projectID": projectID}, &data); err != nil {
		return nil, fmt.Errorf("github: fetch project fields: %w", err)
	}

	var fields []ProjectField
	for _, raw := range data.Node.Fields.Nodes {
		var rf rawField
		if err := json.Unmarshal(raw, &rf); err != nil || rf.Name == "" {
			continue
		}
		f := ProjectField{Name: rf.Name}
		switch {
		case len(rf.Options) > 0:
			f.Type = "single_select"
			for _, o := range rf.Options {
				f.Options = append(f.Options, o.Name)
			}
		case len(rf.Configuration.Iterations) > 0:
			f.Type = "iteration"
			for _, it := range rf.Configuration.Iterations {
				f.Options = append(f.Options, it.Title)
			}
		default:
			f.Type = "text"
		}
		fields = append(fields, f)
	}
	return fields, nil
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
				fieldName := strings.ToLower(strings.ReplaceAll(fv.Field.Name, " ", ""))
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
		var commits []*gh.RepositoryCommit
		var resp *gh.Response
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt*2) * time.Second):
				}
			}
			commits, resp, err = c.client.Repositories.ListCommits(ctx, owner, repo, opts)
			if err == nil {
				break
			}
			if resp != nil && (resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout) {
				log.Printf("WARN  github: list commits %s/%s page %d: %d, retrying (attempt %d/3)", owner, repo, opts.Page, resp.StatusCode, attempt+1)
				continue
			}
			break
		}
		if err != nil {
			// 409 means the repository exists but has no commits yet — treat as empty.
			if resp != nil && resp.StatusCode == http.StatusConflict {
				return result, nil
			}
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

// CloseIssue closes a GitHub issue by number.
func (c *Client) CloseIssue(ctx context.Context, owner, repo string, number int) error {
	if err := c.checkToken(); err != nil {
		return err
	}
	closed := "closed"
	_, _, err := c.client.Issues.Edit(ctx, owner, repo, number, &gh.IssueRequest{State: &closed})
	if err != nil {
		return fmt.Errorf("github: close issue #%d: %w", number, err)
	}
	return nil
}

