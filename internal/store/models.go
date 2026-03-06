package store

import (
	"database/sql"
	"time"
)

// User represents a row in the users table.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

// Team represents a row in the teams table.
type Team struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// TeamMember represents a row in the team_members table.
type TeamMember struct {
	ID           int64
	TeamID       int64
	Name         string
	GithubLogin  sql.NullString
	Role         sql.NullString
	NotionUserID sql.NullString
	CreatedAt    time.Time
}

// SourceCatalogue represents a row in the source_catalogue table.
type SourceCatalogue struct {
	ID           int64
	SourceType   string
	ExternalID   string
	Title        string
	URL          sql.NullString
	SourceMeta   sql.NullString
	AISuggestion sql.NullString
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SourceConfig represents a row in the source_configs table.
type SourceConfig struct {
	ID          int64
	CatalogueID int64
	TeamID      sql.NullInt64
	Purpose     string
	ConfigMeta  sql.NullString
	CreatedAt   time.Time
}

// Annotation represents a row in the annotations table.
type Annotation struct {
	ID        int64
	TeamID    sql.NullInt64
	ItemRef   sql.NullString
	Tier      string
	Content   string
	Archived  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AICache represents a row in the ai_cache table.
type AICache struct {
	ID        int64
	InputHash string
	Pipeline  string
	TeamID    sql.NullInt64
	Output    string
	CreatedAt time.Time
}

// SyncRun represents a row in the sync_runs table.
type SyncRun struct {
	ID         int64
	TeamID     sql.NullInt64
	Scope      string
	Status     string
	Error      sql.NullString
	StartedAt  time.Time
	FinishedAt sql.NullTime
}

// SprintMeta represents a row in the sprint_meta table.
type SprintMeta struct {
	ID           int64
	TeamID       int64
	PlanType     string
	SprintNumber sql.NullInt64
	StartDate    sql.NullString
	EndDate      sql.NullString
	RawContent   sql.NullString
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// RefreshToken represents a row in the refresh_tokens table.
type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}
