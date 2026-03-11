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

// Discover enumerates a github_repo root item, its labels, GitHub ProjectsV2,
// and .md files for the target "owner/repo". The repo item is always first so
// the discovery loop can resolve its catalogue ID before processing children.
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

	// 1. Labels (children of repo).
	labelItems, err := c.discoverLabels(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	for i := range labelItems {
		labelItems[i].ParentExternalID = repoExtID
		labelItems[i].ParentSourceType = "github_repo"
	}
	items = append(items, labelItems...)

	// 2. GitHub Projects (children of repo).
	projectItems, err := c.discoverProjects(ctx, owner, repo)
	if err == nil {
		for i := range projectItems {
			projectItems[i].ParentExternalID = repoExtID
			projectItems[i].ParentSourceType = "github_repo"
		}
		items = append(items, projectItems...)
	}

	// 3. Markdown files (children of repo).
	mdItems, err := c.discoverMarkdownFiles(ctx, owner, repo)
	if err == nil {
		for i := range mdItems {
			mdItems[i].ParentExternalID = repoExtID
			mdItems[i].ParentSourceType = "github_repo"
		}
		items = append(items, mdItems...)
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

	// Discover labels and files from each linked repo.
	// We call discoverLabels/discoverMarkdownFiles directly (not Discover) to
	// avoid re-emitting the project via discoverProjects and creating cycles.
	for _, repo := range proj.Repositories.Nodes {
		repoOwner := repo.Owner.Login
		repoName := repo.Name
		repoExtID := repoOwner + "/" + repoName

		// Repo item as child of project.
		items = append(items, connector.DiscoveredItem{
			SourceType:       "github_repo",
			ExternalID:       repoExtID,
			Title:            repoExtID,
			URL:              fmt.Sprintf("https://github.com/%s/%s", repoOwner, repoName),
			SourceMeta:       map[string]any{"owner": repoOwner, "repo": repoName},
			ParentExternalID: proj.ID,
			ParentSourceType: "github_project",
		})

		// Labels as children of repo.
		labels, err := c.discoverLabels(ctx, repoOwner, repoName)
		if err == nil {
			for i := range labels {
				labels[i].ParentExternalID = repoExtID
				labels[i].ParentSourceType = "github_repo"
			}
			items = append(items, labels...)
		}

		// Markdown files as children of repo.
		mdFiles, err := c.discoverMarkdownFiles(ctx, repoOwner, repoName)
		if err == nil {
			for i := range mdFiles {
				mdFiles[i].ParentExternalID = repoExtID
				mdFiles[i].ParentSourceType = "github_repo"
			}
			items = append(items, mdFiles...)
		}
	}

	return items, nil
}

// FetchIssues fetches issues for a repo relevant to the given label.
// It returns all currently open issues with the label (full sprint board view)
// plus all recently closed issues with the label. The label filter is applied to
// closed issues because autotag enforces label presence on closed project issues,
// so a label-filtered closed search reliably captures completed work without
// pulling in unrelated issues from shared repos.
func (c *Client) FetchIssues(ctx context.Context, owner, repo, label string, since time.Time) ([]*gh.Issue, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	var all []*gh.Issue

	// 1. All open issues with this label — no date filter so we always see the full board.
	if err := c.searchIssues(ctx,
		fmt.Sprintf("repo:%s/%s label:%q is:open", owner, repo, label),
		&all,
	); err != nil {
		return nil, err
	}

	// 2. Recently closed issues with this label. Autotag enforces the label on closed
	// issues, so filtering by label is safe and avoids pulling in unrelated issues
	// from shared repos (e.g. a roadmap repo used by multiple teams).
	closedSince := time.Now().AddDate(0, 0, -90)
	if since.Before(closedSince) {
		closedSince = since
	}
	if err := c.searchIssues(ctx,
		fmt.Sprintf("repo:%s/%s label:%q is:closed closed:>%s", owner, repo, label, closedSince.UTC().Format("2006-01-02")),
		&all,
	); err != nil {
		return nil, err
	}

	return all, nil
}

// searchIssues executes a GitHub issue search query and appends results to dst.
func (c *Client) searchIssues(ctx context.Context, query string, dst *[]*gh.Issue) error {
	opts := &gh.SearchOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		result, resp, err := c.client.Search.Issues(ctx, query, opts)
		if err != nil {
			return fmt.Errorf("github: search issues %q: %w", query, err)
		}
		*dst = append(*dst, result.Issues...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return nil
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

// FetchIssueProjectStatuses takes a list of issue node IDs and returns a map of
// node ID → project Status field value for issues that are members of projectID.
// If projectID is empty, the first Status field value found is used regardless of project.
// Issues not in the project or without a Status value are omitted from the map.
// Node IDs are batched in groups of 100 (GitHub GraphQL nodes() limit).
func (c *Client) FetchIssueProjectStatuses(ctx context.Context, nodeIDs []string, projectID string) (map[string]string, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}
	if len(nodeIDs) == 0 {
		return map[string]string{}, nil
	}

	type projectRef struct {
		ID string `json:"id"`
	}
	type fieldValueNode struct {
		Name  string `json:"name"`
		Field struct {
			Name string `json:"name"`
		} `json:"field"`
	}
	type projectItemNode struct {
		Project     projectRef `json:"project"`
		FieldValues struct {
			Nodes []fieldValueNode `json:"nodes"`
		} `json:"fieldValues"`
	}
	type issueNode struct {
		ID           string `json:"id"`
		ProjectItems struct {
			Nodes []projectItemNode `json:"nodes"`
		} `json:"projectItems"`
	}
	type nodesData struct {
		Nodes []json.RawMessage `json:"nodes"`
	}

	const query = `query($ids: [ID!]!) {
		nodes(ids: $ids) {
			... on Issue {
				id
				projectItems(first: 10) {
					nodes {
						project { id }
						fieldValues(first: 10) {
							nodes {
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

	result := map[string]string{}
	for i := 0; i < len(nodeIDs); i += 100 {
		end := i + 100
		if end > len(nodeIDs) {
			end = len(nodeIDs)
		}
		batch := nodeIDs[i:end]

		var data nodesData
		if err := c.graphql(ctx, query, map[string]any{"ids": batch}, &data); err != nil {
			return nil, err
		}

		for _, raw := range data.Nodes {
			if string(raw) == "null" {
				continue
			}
			var issue issueNode
			if err := json.Unmarshal(raw, &issue); err != nil || issue.ID == "" {
				continue
			}
			for _, item := range issue.ProjectItems.Nodes {
				if projectID != "" && item.Project.ID != projectID {
					continue
				}
				for _, fv := range item.FieldValues.Nodes {
					if strings.EqualFold(fv.Field.Name, "status") && fv.Name != "" {
						result[issue.ID] = fv.Name
						break
					}
				}
				if result[issue.ID] != "" {
					break
				}
			}
		}
	}
	return result, nil
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

// IssueOwnerRepo extracts owner and repo from a gh.Issue by parsing its RepositoryURL
// (e.g. "https://api.github.com/repos/owner/repo") or HTMLURL as a fallback.
func IssueOwnerRepo(issue *gh.Issue) (string, string) {
	if u := issue.GetRepositoryURL(); u != "" {
		// https://api.github.com/repos/{owner}/{repo}
		const prefix = "https://api.github.com/repos/"
		if after, ok := strings.CutPrefix(u, prefix); ok {
			parts := strings.SplitN(after, "/", 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1]
			}
		}
	}
	// Fallback: https://github.com/{owner}/{repo}/issues/{number}
	if u := issue.GetHTMLURL(); u != "" {
		const prefix = "https://github.com/"
		if after, ok := strings.CutPrefix(u, prefix); ok {
			parts := strings.SplitN(after, "/", 3)
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1]
			}
		}
	}
	return "", ""
}

// AutoTagIssues pages all items in the project; for each item with Team/Area set
// but missing the corresponding label on the linked issue, applies the label.
// The owner parameter is used only as a fallback if the issue's repository cannot
// be determined from GraphQL; repo is always resolved per-issue from content.
func (c *Client) AutoTagIssues(ctx context.Context, owner, _, projectID string, teamLabelMap map[string]string) error {
	if err := c.checkToken(); err != nil {
		return err
	}

	type issueContent struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Repository struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
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
				items(first: 100, after: $cursor) {
					pageInfo { hasNextPage endCursor }
					nodes {
						type
						content {
							... on Issue {
								number
								state
								repository {
									name
									owner { login }
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
							}
						}
					}
				}
			}
		}
	}`

	var (
		cursor         *string
		totalItems     int
		alreadyLabeled int
		noTeamArea     int
		noLabel        int
		labeled        int
		labelErrors    int
		unmappedValues = map[string]int{}
	)
	for {
		vars := map[string]any{"projectID": projectID, "cursor": cursor}
		var data projectData
		if err := c.graphql(ctx, query, vars, &data); err != nil {
			return err
		}
		pageItems := len(data.Node.Items.Nodes)
		totalItems += pageItems

		for _, item := range data.Node.Items.Nodes {
			if item.Type != "ISSUE" {
				continue
			}

			// Find Team/Area field value
			teamAreaValue := ""
			for _, fv := range item.FieldValues.Nodes {
				fn := strings.ToLower(strings.ReplaceAll(fv.Field.Name, " ", ""))
				if fn == "team" || fn == "area" || fn == "team/area" {
					if fv.Name != "" {
						teamAreaValue = fv.Name
					}
					break
				}
			}
			if teamAreaValue == "" {
				noTeamArea++
				continue
			}

			// Look up target label
			targetLabel, ok := teamLabelMap[teamAreaValue]
			if !ok {
				unmappedValues[teamAreaValue]++
				noLabel++
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
				alreadyLabeled++
				continue
			}

			issueNum := item.Content.Number
			if issueNum == 0 {
				continue
			}

			// Resolve owner/repo from the issue itself; fall back to the caller-provided owner.
			issueOwner := item.Content.Repository.Owner.Login
			issueRepo := item.Content.Repository.Name
			if issueOwner == "" {
				issueOwner = owner
			}
			if issueOwner == "" || issueRepo == "" {
				log.Printf("autotag: skip issue #%d: cannot determine repository", issueNum)
				continue
			}

			t0 := time.Now()
			_, _, err := c.client.Issues.AddLabelsToIssue(ctx, issueOwner, issueRepo, issueNum, []string{targetLabel})
			if err != nil {
				labelErrors++
				log.Printf("autotag: error labeling issue #%d (%s/%s) with %q: %v (%.2fs)", issueNum, issueOwner, issueRepo, targetLabel, err, time.Since(t0).Seconds())
				continue
			}
			labeled++
			log.Printf("autotag: labeled issue #%d (%s/%s) with %q in %.2fs", issueNum, issueOwner, issueRepo, targetLabel, time.Since(t0).Seconds())
		}

		if !data.Node.Items.PageInfo.HasNextPage {
			break
		}
		c2 := data.Node.Items.PageInfo.EndCursor
		cursor = &c2
	}
	if len(unmappedValues) > 0 {
		log.Printf("autotag: %d items — %d labeled, %d already had label, %d no team/area, %d unmapped %v, %d errors",
			totalItems, labeled, alreadyLabeled, noTeamArea, noLabel, unmappedValues, labelErrors)
	} else {
		log.Printf("autotag: %d items — %d labeled, %d already had label, %d no team/area, %d errors",
			totalItems, labeled, alreadyLabeled, noTeamArea, labelErrors)
	}
	return nil
}
