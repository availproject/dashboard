package notion

import (
	"context"
	"fmt"
	"strings"
	"time"

	notion "github.com/jomei/notionapi"
	"github.com/your-org/dashboard/internal/connector"
)

// Client wraps the jomei/notionapi client.
type Client struct {
	nc    *notion.Client
	token string
}

// DatabaseRow represents a single row from a Notion database query.
type DatabaseRow struct {
	Title   string
	Content string
}

// New creates a Notion Client with the provided integration token.
// If token is empty, methods will return an error.
func New(token string) *Client {
	if token == "" {
		return &Client{}
	}
	nc := notion.NewClient(notion.Token(token))
	return &Client{nc: nc, token: token}
}

func (c *Client) checkToken() error {
	if c.token == "" || c.nc == nil {
		return connector.NewErrCredentialsMissing("NOTION_TOKEN")
	}
	return nil
}

// Discover searches the Notion workspace and returns one DiscoveredItem per
// page (notion_page) and database (notion_db). The workspaceHint parameter is
// unused but satisfies the Discoverer interface.
func (c *Client) Discover(ctx context.Context, workspaceHint string) ([]connector.DiscoveredItem, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	var items []connector.DiscoveredItem
	var cursor notion.Cursor

	for {
		req := &notion.SearchRequest{
			PageSize: 100,
		}
		if cursor != "" {
			req.StartCursor = cursor
		}

		resp, err := c.nc.Search.Do(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("notion: search failed: %w", err)
		}

		for _, obj := range resp.Results {
			switch v := obj.(type) {
			case *notion.Page:
				title := extractPageTitle(v)
				items = append(items, connector.DiscoveredItem{
					SourceType: "notion_page",
					ExternalID: v.ID.String(),
					Title:      title,
					URL:        v.URL,
				})
			case *notion.Database:
				title := extractDatabaseTitle(v)
				items = append(items, connector.DiscoveredItem{
					SourceType: "notion_db",
					ExternalID: v.ID.String(),
					Title:      title,
					URL:        v.URL,
				})
			}
		}

		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}

	return items, nil
}

// FetchPage retrieves a Notion page's content as plain text and returns the
// last edited time.
func (c *Client) FetchPage(ctx context.Context, pageID string) (string, time.Time, error) {
	if err := c.checkToken(); err != nil {
		return "", time.Time{}, err
	}

	page, err := c.nc.Page.Get(ctx, notion.PageID(pageID))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("notion: get page %s: %w", pageID, err)
	}

	content, err := c.fetchBlocksText(ctx, notion.BlockID(pageID), 0)
	if err != nil {
		return "", time.Time{}, err
	}

	return content, page.LastEditedTime, nil
}

// FetchDatabase queries a Notion database for rows updated after updatedAfter
// and returns typed DatabaseRow values including linked page content.
func (c *Client) FetchDatabase(ctx context.Context, dbID string, updatedAfter time.Time) ([]DatabaseRow, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}

	filter := notion.TimestampFilter{
		Timestamp: notion.TimestampLastEdited,
		LastEditedTime: &notion.DateFilterCondition{
			After: (*notion.Date)(timePtr(updatedAfter)),
		},
	}

	var rows []DatabaseRow
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
			return nil, fmt.Errorf("notion: query database %s: %w", dbID, err)
		}

		for _, page := range resp.Results {
			title := extractPageTitle(&page)

			content, err := c.fetchBlocksText(ctx, notion.BlockID(page.ID.String()), 0)
			if err != nil {
				content = ""
			}

			rows = append(rows, DatabaseRow{
				Title:   title,
				Content: content,
			})
		}

		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}

	return rows, nil
}

// fetchBlocksText recursively retrieves block children and converts them to
// plain text with markdown-like formatting.
func (c *Client) fetchBlocksText(ctx context.Context, blockID notion.BlockID, depth int) (string, error) {
	var sb strings.Builder
	var cursor string

	for {
		var pagination *notion.Pagination
		if cursor != "" {
			pagination = &notion.Pagination{StartCursor: notion.Cursor(cursor)}
		}

		resp, err := c.nc.Block.GetChildren(ctx, blockID, pagination)
		if err != nil {
			return "", fmt.Errorf("notion: get children of block %s: %w", blockID, err)
		}

		for _, block := range resp.Results {
			line := blockToText(block)
			if line != "" {
				sb.WriteString(line)
				sb.WriteString("\n")
			}

			if block.GetHasChildren() {
				child, err := c.fetchBlocksText(ctx, block.GetID(), depth+1)
				if err != nil {
					return "", err
				}
				if child != "" {
					sb.WriteString(child)
				}
			}
		}

		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}

	return sb.String(), nil
}

// blockToText converts a single Notion block to a plain-text line.
func blockToText(block notion.Block) string {
	switch b := block.(type) {
	case *notion.Heading1Block:
		text := concatenateRichText(b.Heading1.RichText)
		if text != "" {
			return "# " + text
		}
	case *notion.Heading2Block:
		text := concatenateRichText(b.Heading2.RichText)
		if text != "" {
			return "# " + text
		}
	case *notion.Heading3Block:
		text := concatenateRichText(b.Heading3.RichText)
		if text != "" {
			return "# " + text
		}
	case *notion.BulletedListItemBlock:
		text := concatenateRichText(b.BulletedListItem.RichText)
		if text != "" {
			return "- " + text
		}
	case *notion.NumberedListItemBlock:
		text := concatenateRichText(b.NumberedListItem.RichText)
		if text != "" {
			return "- " + text
		}
	case *notion.ParagraphBlock:
		return concatenateRichText(b.Paragraph.RichText)
	case *notion.ToggleBlock:
		text := concatenateRichText(b.Toggle.RichText)
		if text != "" {
			return "> " + text
		}
	case *notion.CalloutBlock:
		return concatenateRichText(b.Callout.RichText)
	case *notion.QuoteBlock:
		return concatenateRichText(b.Quote.RichText)
	case *notion.ToDoBlock:
		return concatenateRichText(b.ToDo.RichText)
	}
	return ""
}

func concatenateRichText(rts []notion.RichText) string {
	var sb strings.Builder
	for _, rt := range rts {
		sb.WriteString(rt.PlainText)
	}
	return sb.String()
}

func extractPageTitle(page *notion.Page) string {
	for _, prop := range page.Properties {
		if tp, ok := prop.(*notion.TitleProperty); ok {
			return concatenateRichText(tp.Title)
		}
	}
	return ""
}

func extractDatabaseTitle(db *notion.Database) string {
	return concatenateRichText(db.Title)
}

func timePtr(t time.Time) *time.Time {
	return &t
}
