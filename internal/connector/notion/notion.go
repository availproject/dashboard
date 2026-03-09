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

// Discover returns DiscoveredItems from Notion.
//
// If workspaceHint looks like a URL (starts with "http"), scoped discovery is
// performed: the page identified by the URL is fetched and its child pages /
// databases are walked recursively up to maxDiscoverDepth levels deep.
//
// Otherwise a workspace-wide search is performed (two API calls — one for
// pages, one for databases — because the jomei/notionapi library serialises an
// empty Filter as {"property":""}, which the Notion API rejects).
func (c *Client) Discover(ctx context.Context, workspaceHint string) ([]connector.DiscoveredItem, error) {
	if err := c.checkToken(); err != nil {
		return nil, err
	}
	if strings.HasPrefix(workspaceHint, "http") {
		return c.discoverScoped(ctx, workspaceHint)
	}
	pages, err := c.searchByType(ctx, "page")
	if err != nil {
		return nil, err
	}
	dbs, err := c.searchByType(ctx, "database")
	if err != nil {
		return nil, err
	}
	return append(pages, dbs...), nil
}

const maxDiscoverDepth = 4

// discoverScoped discovers pages nested under the Notion page identified by
// pageURL. The root page itself is included in the results.
func (c *Client) discoverScoped(ctx context.Context, pageURL string) ([]connector.DiscoveredItem, error) {
	pageID, err := parseNotionPageID(pageURL)
	if err != nil {
		return nil, fmt.Errorf("notion: parse page URL: %w", err)
	}

	page, err := c.nc.Page.Get(ctx, notion.PageID(pageID))
	if err != nil {
		return nil, fmt.Errorf("notion: get root page: %w", err)
	}

	items := []connector.DiscoveredItem{{
		SourceType: "notion_page",
		ExternalID: page.ID.String(),
		Title:      extractPageTitle(page),
		URL:        page.URL,
	}}

	children, err := c.walkChildPages(ctx, notion.BlockID(page.ID.String()), page.ID.String(), "notion_page", 0)
	if err != nil {
		return nil, err
	}
	return append(items, children...), nil
}

// walkChildPages recursively collects child_page and child_database blocks
// under blockID, up to maxDiscoverDepth levels of nesting.
// parentExternalID and parentSourceType identify the parent item in the catalogue.
func (c *Client) walkChildPages(ctx context.Context, blockID notion.BlockID, parentExternalID, parentSourceType string, depth int) ([]connector.DiscoveredItem, error) {
	if depth >= maxDiscoverDepth {
		return nil, nil
	}

	var items []connector.DiscoveredItem
	var cursor string

	for {
		var pagination *notion.Pagination
		if cursor != "" {
			pagination = &notion.Pagination{StartCursor: notion.Cursor(cursor)}
		}

		resp, err := c.nc.Block.GetChildren(ctx, blockID, pagination)
		if err != nil {
			return nil, fmt.Errorf("notion: get children of %s: %w", blockID, err)
		}

		for _, block := range resp.Results {
			switch b := block.(type) {
			case *notion.ChildPageBlock:
				id := b.GetID().String()
				items = append(items, connector.DiscoveredItem{
					SourceType:       "notion_page",
					ExternalID:       id,
					Title:            b.ChildPage.Title,
					URL:              "https://www.notion.so/" + strings.ReplaceAll(id, "-", ""),
					ParentExternalID: parentExternalID,
					ParentSourceType: parentSourceType,
				})
				children, err := c.walkChildPages(ctx, b.GetID(), id, "notion_page", depth+1)
				if err != nil {
					return nil, err
				}
				items = append(items, children...)
			case *notion.ChildDatabaseBlock:
				id := b.GetID().String()
				items = append(items, connector.DiscoveredItem{
					SourceType:       "notion_db",
					ExternalID:       id,
					Title:            b.ChildDatabase.Title,
					URL:              "https://www.notion.so/" + strings.ReplaceAll(id, "-", ""),
					ParentExternalID: parentExternalID,
					ParentSourceType: parentSourceType,
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

// parseNotionPageID extracts a Notion page UUID (with dashes) from a page URL.
// Notion URLs end with an optional title slug followed by a 32-char hex ID,
// e.g. https://www.notion.so/workspace/My-Title-2e8e67c666dd81c283fcdb0189eb50e5
func parseNotionPageID(rawURL string) (string, error) {
	u := strings.TrimRight(rawURL, "/")
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	i := strings.LastIndex(u, "/")
	if i < 0 {
		return "", fmt.Errorf("invalid Notion URL: %s", rawURL)
	}
	seg := u[i+1:]
	if len(seg) < 32 {
		return "", fmt.Errorf("cannot find page ID in %q", seg)
	}
	raw := seg[len(seg)-32:]
	for _, ch := range raw {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return "", fmt.Errorf("invalid page ID chars in %q", raw)
		}
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", raw[:8], raw[8:12], raw[12:16], raw[16:20], raw[20:]), nil
}

func (c *Client) searchByType(ctx context.Context, objectType string) ([]connector.DiscoveredItem, error) {
	var items []connector.DiscoveredItem
	var cursor notion.Cursor

	for {
		req := &notion.SearchRequest{
			PageSize: 100,
			Filter: notion.SearchFilter{
				Property: "object",
				Value:    objectType,
			},
		}
		if cursor != "" {
			req.StartCursor = cursor
		}

		resp, err := c.nc.Search.Do(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("notion: search %s failed: %w", objectType, err)
		}

		for _, obj := range resp.Results {
			switch v := obj.(type) {
			case *notion.Page:
				items = append(items, connector.DiscoveredItem{
					SourceType: "notion_page",
					ExternalID: v.ID.String(),
					Title:      extractPageTitle(v),
					URL:        v.URL,
				})
			case *notion.Database:
				items = append(items, connector.DiscoveredItem{
					SourceType: "notion_db",
					ExternalID: v.ID.String(),
					Title:      extractDatabaseTitle(v),
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

// FetchPageIfChanged fetches a Notion page only if it has been edited after
// knownLastEdited. If unchanged, content is "" and changed is false.
// Pass an empty knownLastEdited to always fetch.
func (c *Client) FetchPageIfChanged(ctx context.Context, pageID, knownLastEdited string) (content, lastEdited string, changed bool, err error) {
	if err := c.checkToken(); err != nil {
		return "", "", false, err
	}

	page, err := c.nc.Page.Get(ctx, notion.PageID(pageID))
	if err != nil {
		return "", "", false, fmt.Errorf("notion: get page %s: %w", pageID, err)
	}

	pageLastEdited := page.LastEditedTime.UTC().Format(time.RFC3339)
	if knownLastEdited != "" && pageLastEdited == knownLastEdited {
		return "", pageLastEdited, false, nil
	}

	text, err := c.fetchBlocksText(ctx, notion.BlockID(pageID), 0)
	if err != nil {
		return "", "", false, err
	}
	return text, pageLastEdited, true, nil
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

			if block.GetHasChildren() && blockChildrenUseful(block) {
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
// Child page and bookmark blocks emit their URLs so that AI pipelines
// (e.g. homepage_extract) can extract links.
func blockToText(block notion.Block) string {
	switch b := block.(type) {
	case *notion.Heading1Block:
		text := richTextToMarkdown(b.Heading1.RichText)
		if text != "" {
			return "# " + text
		}
	case *notion.Heading2Block:
		text := richTextToMarkdown(b.Heading2.RichText)
		if text != "" {
			return "## " + text
		}
	case *notion.Heading3Block:
		text := richTextToMarkdown(b.Heading3.RichText)
		if text != "" {
			return "### " + text
		}
	case *notion.BulletedListItemBlock:
		text := richTextToMarkdown(b.BulletedListItem.RichText)
		if text != "" {
			return "- " + text
		}
	case *notion.NumberedListItemBlock:
		text := richTextToMarkdown(b.NumberedListItem.RichText)
		if text != "" {
			return "- " + text
		}
	case *notion.ParagraphBlock:
		return richTextToMarkdown(b.Paragraph.RichText)
	case *notion.ToggleBlock:
		text := richTextToMarkdown(b.Toggle.RichText)
		if text != "" {
			return "> " + text
		}
	case *notion.CalloutBlock:
		return richTextToMarkdown(b.Callout.RichText)
	case *notion.QuoteBlock:
		return richTextToMarkdown(b.Quote.RichText)
	case *notion.ToDoBlock:
		return richTextToMarkdown(b.ToDo.RichText)
	case *notion.ChildPageBlock:
		// Child pages: emit a markdown link using the Notion page URL so AI
		// pipelines can extract the link to the subpage.
		pageID := strings.ReplaceAll(block.GetID().String(), "-", "")
		url := "https://www.notion.so/" + pageID
		title := b.ChildPage.Title
		if title == "" {
			title = "Untitled"
		}
		return "[" + title + "](" + url + ")"
	case *notion.BookmarkBlock:
		// Bookmark blocks are external URLs embedded in the page.
		if b.Bookmark.URL != "" {
			caption := concatenateRichText(b.Bookmark.Caption)
			if caption != "" {
				return "[" + caption + "](" + b.Bookmark.URL + ")"
			}
			return b.Bookmark.URL
		}
	case *notion.LinkToPageBlock:
		// Link-to-page blocks reference another Notion page by ID.
		if b.LinkToPage.PageID != "" {
			pageID := strings.ReplaceAll(string(b.LinkToPage.PageID), "-", "")
			return "https://www.notion.so/" + pageID
		}
	}
	return ""
}

// blockChildrenUseful reports whether recursing into a block's children would
// yield additional text worth including. Child pages and link-to-page blocks
// are already fully represented by their title/URL from blockToText, so
// fetching their subtrees is wasteful. Block types not handled by blockToText
// (tables, columns, images, etc.) produce no output anyway.
func blockChildrenUseful(block notion.Block) bool {
	switch block.(type) {
	case *notion.ChildPageBlock,
		*notion.ChildDatabaseBlock,
		*notion.LinkToPageBlock,
		*notion.TableBlock,
		*notion.ColumnListBlock,
		*notion.ColumnBlock,
		*notion.ImageBlock,
		*notion.VideoBlock,
		*notion.FileBlock,
		*notion.PdfBlock,
		*notion.BookmarkBlock,
		*notion.DividerBlock,
		*notion.EmbedBlock:
		return false
	}
	return true
}

// richTextToMarkdown converts rich text spans to a string, preserving hrefs
// as markdown links: [text](url). This ensures that inline Notion page
// mentions and external hyperlinks are visible as URLs to AI pipelines.
func richTextToMarkdown(rts []notion.RichText) string {
	var sb strings.Builder
	for _, rt := range rts {
		if rt.Href != "" && rt.PlainText != rt.Href {
			sb.WriteString("[" + rt.PlainText + "](" + rt.Href + ")")
		} else {
			sb.WriteString(rt.PlainText)
		}
	}
	return sb.String()
}

// concatenateRichText returns plain text only (no href expansion).
// Used where URLs in text would be noise (e.g. database row content).
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
