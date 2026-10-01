package domain

import (
	"encoding/json"
	"time"
)

const DefaultProfileID = "00000000-0000-4000-8000-000000000001"

type Feed struct {
	ID               string     `json:"id"`
	URL              string     `json:"url"`
	CanonicalURL     string     `json:"canonical_url"`
	SiteURL          *string    `json:"site_url"`
	Title            string     `json:"title"`
	Description      *string    `json:"description"`
	IconURL          *string    `json:"icon_url"`
	Format           *string    `json:"format"`
	ContentKind      string     `json:"content_kind"`
	ETag             *string    `json:"etag,omitempty"`
	LastModified     *string    `json:"last_modified,omitempty"`
	LastCheckedAt    *time.Time `json:"last_checked_at"`
	LastSuccessAt    *time.Time `json:"last_success_at"`
	NextCheckAt      *time.Time `json:"next_check_at,omitempty"`
	FailureCount     int        `json:"failure_count"`
	LastErrorCode    *string    `json:"last_error_code"`
	LastErrorMessage *string    `json:"last_error_message"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Subscription struct {
	ID                     string    `json:"id"`
	ProfileID              string    `json:"-"`
	FeedID                 string    `json:"feed_id"`
	FolderID               *string   `json:"folder_id"`
	Position               int       `json:"position"`
	Title                  string    `json:"title"`
	IconURL                *string   `json:"icon_url"`
	FeedURL                string    `json:"feed_url"`
	SiteURL                *string   `json:"site_url"`
	UnreadCount            int       `json:"unread_count"`
	FailureCount           int       `json:"failure_count"`
	LastErrorCode          *string   `json:"last_error_code"`
	LastErrorMessage       *string   `json:"last_error_message"`
	LastSuccessAt          *string   `json:"last_success_at"`
	ViewMode               string    `json:"view_mode"`
	RefreshPolicy          string    `json:"refresh_policy"`
	RefreshIntervalMinutes int       `json:"refresh_interval_minutes"`
	HideFromTimeline       bool      `json:"hide_from_timeline"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type SubscriptionPatch struct {
	SetFolderID            bool
	FolderID               *string
	SetTitleOverride       bool
	TitleOverride          *string
	ViewMode               *string
	RefreshPolicy          *string
	RefreshIntervalMinutes *int
	Position               *int
	HideFromTimeline       *bool
}

type Entry struct {
	ID                string     `json:"id"`
	FeedID            string     `json:"feed_id"`
	FeedTitle         string     `json:"feed_title"`
	GUID              *string    `json:"guid,omitempty"`
	CanonicalURL      *string    `json:"canonical_url"`
	Title             string     `json:"title"`
	Author            *string    `json:"author"`
	Summary           *string    `json:"summary"`
	PublishedAt       time.Time  `json:"published_at"`
	DiscoveredAt      time.Time  `json:"discovered_at"`
	ContentHash       string     `json:"-"`
	LeadImageURL      *string    `json:"lead_image_url"`
	AudioURL          *string    `json:"audio_url,omitempty"`
	VideoURL          *string    `json:"video_url,omitempty"`
	Language          *string    `json:"language,omitempty"`
	DOI               *string    `json:"doi,omitempty"`
	AITranslatedTitle *string    `json:"ai_translated_title,omitempty"`
	AISummary         *string    `json:"ai_summary,omitempty"`
	TagIDs            []string   `json:"tag_ids"`
	State             EntryState `json:"state"`
	SanitizedHTML     string     `json:"sanitized_html,omitempty"`
	ReadabilityHTML   *string    `json:"readability_html,omitempty"`
}

type EntryState struct {
	IsRead      bool      `json:"is_read"`
	IsStarred   bool      `json:"is_starred"`
	IsReadLater bool      `json:"is_read_later"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type EntryAnnotation struct {
	ID        string    `json:"id"`
	EntryID   string    `json:"entry_id"`
	Style     string    `json:"style"`
	Quote     string    `json:"quote"`
	Prefix    string    `json:"prefix"`
	Suffix    string    `json:"suffix"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type EntryAnnotationPatch struct {
	Style *string
	Note  *string
}

type EntryPage struct {
	Items      []Entry `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type EntryFilter struct {
	ProfileID   string
	FeedID      string
	FolderID    string
	TagID       string
	State       string
	Query       string
	ContentKind string
	Cursor      string
	Limit       int
	Since       *time.Time
	AILanguage  string
}

type EntryStatePatch struct {
	MutationID  string
	DeviceID    *string
	IsRead      *bool
	IsStarred   *bool
	IsReadLater *bool
	DeviceTime  *time.Time
}

type ParsedFeed struct {
	Title       string
	Description *string
	SiteURL     *string
	IconURL     *string
	Format      string
	Entries     []ParsedEntry
}

type ParsedEntry struct {
	GUID          *string
	CanonicalURL  *string
	Title         string
	Author        *string
	Summary       *string
	PublishedAt   time.Time
	ContentHash   string
	IdentityHash  string
	SourceHTML    string
	SanitizedHTML string
	PlainText     string
	LeadImageURL  *string
	AudioURL      *string
	VideoURL      *string
	Language      *string
	DOI           *string
}

type FeedCandidate struct {
	URL     string  `json:"url"`
	Title   string  `json:"title"`
	SiteURL *string `json:"site_url"`
}

type Job struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	State           string     `json:"state"`
	PayloadJSON     string     `json:"-"`
	ProgressCurrent int        `json:"progress_current"`
	ProgressTotal   int        `json:"progress_total"`
	ScheduledAt     time.Time  `json:"scheduled_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	ErrorCode       *string    `json:"error_code"`
	ErrorMessage    *string    `json:"error_message"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Folder struct {
	ID        string    `json:"id"`
	ProfileID string    `json:"-"`
	ParentID  *string   `json:"parent_id"`
	Name      string    `json:"name"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Tag struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     *string   `json:"color"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

type Rule struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Enabled        bool            `json:"enabled"`
	Priority       int             `json:"priority"`
	ConditionsJSON json.RawMessage `json:"conditions"`
	ActionsJSON    json.RawMessage `json:"actions"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type SavedFilter struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	QueryJSON json.RawMessage `json:"query"`
	Position  int             `json:"position"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Device struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

type SyncAccount struct {
	ID                  string     `json:"id"`
	Provider            string     `json:"provider"`
	Name                string     `json:"name"`
	Endpoint            string     `json:"endpoint"`
	Enabled             bool       `json:"enabled"`
	AllowPrivateNetwork bool       `json:"allow_private_network"`
	SyncIntervalMinutes int        `json:"sync_interval_minutes"`
	LastSyncAt          *time.Time `json:"last_sync_at"`
	NextSyncAt          *time.Time `json:"next_sync_at"`
	LastAttemptAt       *time.Time `json:"last_attempt_at"`
	LastErrorCode       *string    `json:"last_error_code"`
	LastErrorMessage    *string    `json:"last_error_message"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type AIProfile struct {
	ID                    string     `json:"id"`
	Provider              string     `json:"provider"`
	Name                  string     `json:"name"`
	Endpoint              string     `json:"endpoint"`
	Model                 string     `json:"model"`
	Enabled               bool       `json:"enabled"`
	AllowPrivateNetwork   bool       `json:"allow_private_network"`
	RemoteContentApproved bool       `json:"remote_content_approved"`
	IsDefault             bool       `json:"is_default"`
	LastUsedAt            *time.Time `json:"last_used_at"`
	LastErrorCode         *string    `json:"last_error_code"`
	LastErrorMessage      *string    `json:"last_error_message"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type AIUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type AIResult struct {
	ID          string          `json:"id"`
	AIProfileID *string         `json:"ai_profile_id"`
	EntryID     string          `json:"entry_id"`
	Operation   string          `json:"operation"`
	Language    string          `json:"language"`
	InputHash   string          `json:"input_hash"`
	ResultText  string          `json:"result_text"`
	Usage       json.RawMessage `json:"usage"`
	CreatedAt   time.Time       `json:"created_at"`
}

type AIChatSession struct {
	ID          string          `json:"id"`
	AIProfileID *string         `json:"ai_profile_id"`
	EntryID     *string         `json:"entry_id"`
	Title       string          `json:"title"`
	Messages    []AIChatMessage `json:"messages,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type AIChatMessage struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata"`
	Status    string          `json:"status"`
	Usage     json.RawMessage `json:"usage"`
	CreatedAt time.Time       `json:"created_at"`
}

// Research workspace kinds.
const (
	ResearchKindResearch  = "research"
	ResearchKindSubmitted = "submitted"
	ResearchKindPublished = "published"
)

// ResearchStage is one node in a paper's up-to-three-level progress tree.
type ResearchStage struct {
	Name     string          `json:"name"`
	Done     bool            `json:"done"`
	Children []ResearchStage `json:"children"`
}

// SubmissionRecord is one entry in a submitted paper's history timeline.
type SubmissionRecord struct {
	Journal string `json:"journal"`
	Date    string `json:"date"`
	Status  string `json:"status"`
}

// ResearchPaper is a working paper, submission, or publication tracked in the
// research workspace. Kind-specific fields stay zero-valued for other kinds.
type ResearchPaper struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Position   int      `json:"position"`
	Title      string   `json:"title"`
	Authors    []string `json:"authors"`
	Keywords   []string `json:"keywords"`
	FilePath   string   `json:"file_path"`
	NextAction string   `json:"next_action"`
	Notes      string   `json:"notes"`

	// research
	ResearchArea  string          `json:"research_area"`
	Status        string          `json:"status"`
	TagID         string          `json:"tag_id"`
	TargetJournal string          `json:"target_journal"`
	Stages        []ResearchStage `json:"stages"`

	// submitted
	CurrentJournal  string             `json:"current_journal"`
	SubmissionDate  string             `json:"submission_date"`
	ManuscriptID    string             `json:"manuscript_id"`
	SubmissionCount int                `json:"submission_count"`
	TargetLevel     string             `json:"target_level"`
	Editor          string             `json:"editor"`
	Deadline        string             `json:"deadline"`
	History         []SubmissionRecord `json:"history"`

	// published
	Abstract          string `json:"abstract"`
	Journal           string `json:"journal"`
	Language          string `json:"language"`
	Year              string `json:"year"`
	Volume            string `json:"volume"`
	Issue             string `json:"issue"`
	Pages             string `json:"pages"`
	DOI               string `json:"doi"`
	Citations         *int   `json:"citations"`
	CitationSource    string `json:"citation_source"`
	CitationUpdatedAt string `json:"citation_updated_at"`

	LastUpdated string    `json:"last_updated"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ResearchTag is one label in a profile's ordered, user-defined palette. It
// replaced the fixed High/Medium/Average priority enum: position is the tag's
// priority order, and papers reference tags by id so renames stay cheap.
type ResearchTag struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// ResearchPaperPatch carries optional field updates; nil fields are untouched.
type ResearchPaperPatch struct {
	Title           *string
	Authors         *[]string
	Keywords        *[]string
	FilePath        *string
	NextAction      *string
	Notes           *string
	ResearchArea    *string
	Status          *string
	TagID           *string
	TargetJournal   *string
	Stages          *[]ResearchStage
	CurrentJournal  *string
	SubmissionDate  *string
	ManuscriptID    *string
	SubmissionCount *int
	TargetLevel     *string
	Editor          *string
	Deadline        *string
	History         *[]SubmissionRecord
	Abstract        *string
	Journal         *string
	Language        *string
	Year            *string
	Volume          *string
	Issue           *string
	Pages           *string
	DOI             *string
	SetCitations    bool
	Citations       *int
	CitationSource  *string
}
