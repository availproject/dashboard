package notion

import (
	"context"
	"fmt"
	"time"

	notion "github.com/jomei/notionapi"
)

// Property names for the shared marketing calendar Notion database.
const (
	mktPropTitle   = "Project Campaign"
	mktPropStatus  = "Status"
	mktPropDate    = "Date"
	mktPropProject = "Project "
	mktPropTasks   = "Campaign Tasks"
)

// MarketingCampaign holds data for a single campaign row from the marketing calendar DB.
type MarketingCampaign struct {
	PageID    string
	Title     string
	Status    string
	DateStart *time.Time
	DateEnd   *time.Time
	Tasks     []MarketingTask
}

// MarketingTask holds data for a single task page linked from a campaign.
type MarketingTask struct {
	PageID   string
	Title    string
	Status   string
	Assignee string
	Body     string
}

// FetchProjectLabels returns the available option names for the "Project " select
// field in the given Notion database. These are the values that can be used as
// the marketing_label filter on a team.
func (c *Client) FetchProjectLabels(ctx context.Context, dbID string) ([]string, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}
	db, err := c.nc.Database.Get(ctx, notion.DatabaseID(dbID))
	if err != nil {
		return nil, fmt.Errorf("notion: get database %s: %w", dbID, err)
	}
	prop, ok := db.Properties[mktPropProject]
	if !ok {
		return nil, fmt.Errorf("notion: property %q not found in database %s", mktPropProject, dbID)
	}
	sc, ok := prop.(*notion.SelectPropertyConfig)
	if !ok {
		return nil, fmt.Errorf("notion: property %q is not a select field", mktPropProject)
	}
	labels := make([]string, 0, len(sc.Select.Options))
	for _, opt := range sc.Select.Options {
		if opt.Name != "" {
			labels = append(labels, opt.Name)
		}
	}
	return labels, nil
}

// FetchMarketingCampaigns queries the marketing calendar database for campaigns
// matching projectLabel with status "In Progress" or "Not Started".
// For each campaign, linked task pages are fetched individually.
// Note: relation properties with >100 items are not paginated (practically never occurs for campaigns).
func (c *Client) FetchMarketingCampaigns(ctx context.Context, dbID, projectLabel string) ([]MarketingCampaign, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	filter := notion.AndCompoundFilter{
		notion.OrCompoundFilter{
			notion.PropertyFilter{
				Property: mktPropStatus,
				Status:   &notion.StatusFilterCondition{Equals: "In Progress"},
			},
			notion.PropertyFilter{
				Property: mktPropStatus,
				Status:   &notion.StatusFilterCondition{Equals: "Not Started"},
			},
		},
		notion.PropertyFilter{
			Property: mktPropProject,
			Select:   &notion.SelectFilterCondition{Equals: projectLabel},
		},
	}

	var campaigns []MarketingCampaign
	var cursor notion.Cursor

	for {
		req := &notion.DatabaseQueryRequest{
			Filter:   filter,
			PageSize: 100,
		}
		if cursor != "" {
			req.StartCursor = cursor
		}

		resp, err := c.nc.Database.Query(ctx, notion.DatabaseID(dbID), req)
		if err != nil {
			return nil, fmt.Errorf("notion: query marketing DB %s: %w", dbID, err)
		}

		for _, page := range resp.Results {
			campaign := extractCampaign(&page)
			for _, taskID := range extractRelationIDs(&page, mktPropTasks) {
				task, err := c.fetchMarketingTask(ctx, taskID)
				if err != nil {
					// Non-fatal: skip inaccessible task pages.
					continue
				}
				campaign.Tasks = append(campaign.Tasks, task)
			}
			campaigns = append(campaigns, campaign)
		}

		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}

	return campaigns, nil
}

func extractCampaign(page *notion.Page) MarketingCampaign {
	c := MarketingCampaign{
		PageID: page.ID.String(),
		Title:  extractPageTitle(page),
		Status: extractStatusOrSelect(page.Properties, mktPropStatus),
	}
	if dp, ok := page.Properties[mktPropDate].(*notion.DateProperty); ok && dp.Date != nil {
		if dp.Date.Start != nil {
			t := time.Time(*dp.Date.Start)
			c.DateStart = &t
		}
		if dp.Date.End != nil {
			t := time.Time(*dp.Date.End)
			c.DateEnd = &t
		}
	}
	return c
}

func extractRelationIDs(page *notion.Page, propName string) []string {
	rel, ok := page.Properties[propName].(*notion.RelationProperty)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(rel.Relation))
	for _, r := range rel.Relation {
		ids = append(ids, r.ID.String())
	}
	return ids
}

func (c *Client) fetchMarketingTask(ctx context.Context, pageID string) (MarketingTask, error) {
	page, err := c.nc.Page.Get(ctx, notion.PageID(pageID))
	if err != nil {
		return MarketingTask{}, fmt.Errorf("notion: get task page %s: %w", pageID, err)
	}

	body, _ := c.fetchBlocksText(ctx, notion.BlockID(pageID), 0)
	if len(body) > 500 {
		body = body[:500]
	}

	return MarketingTask{
		PageID:   pageID,
		Title:    extractPageTitle(page),
		Status:   extractStatusOrSelect(page.Properties, "Status", "Task Status", "State", "Stage", "Progress"),
		Assignee: extractPeopleProperty(page.Properties, "Owner", "Assignee", "Assigned To", "BD / Product Rep", "Person", "DRI", "Team"),
		Body:     body,
	}, nil
}

// extractStatusOrSelect tries StatusProperty then SelectProperty for each given name in order.
func extractStatusOrSelect(props notion.Properties, names ...string) string {
	for _, name := range names {
		prop, ok := props[name]
		if !ok {
			continue
		}
		switch p := prop.(type) {
		case *notion.StatusProperty:
			if p.Status.Name != "" {
				return p.Status.Name
			}
		case *notion.SelectProperty:
			if p.Select.Name != "" {
				return p.Select.Name
			}
		}
	}
	return ""
}

// extractPeopleProperty returns the display name of the first person in any of the given property names.
func extractPeopleProperty(props notion.Properties, names ...string) string {
	for _, name := range names {
		prop, ok := props[name]
		if !ok {
			continue
		}
		if pp, ok := prop.(*notion.PeopleProperty); ok {
			for _, u := range pp.People {
				if u.Name != "" {
					return u.Name
				}
			}
		}
	}
	return ""
}
