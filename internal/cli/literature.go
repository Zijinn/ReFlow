package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

// Literature identification is deliberately deterministic: a DOI on the entry, the
// category the server gave its feed, and whether the feed named authors. The
// caller is an AI agent, so the CLI hands over evidence and a confidence tier and
// leaves the judgement to it rather than hiding rows behind a score nobody can
// inspect.
const (
	confidenceHigh   = "high"
	confidenceMedium = "medium"
	confidenceLow    = "low"
)

var confidenceTiers = []string{confidenceHigh, confidenceMedium, confidenceLow}

type candidate struct {
	EntryID         string        `json:"entry_id"`
	Title           string        `json:"title"`
	Authors         []string      `json:"authors"`
	DOI             string        `json:"doi,omitempty"`
	CanonicalURL    string        `json:"canonical_url,omitempty"`
	PublishedAt     string        `json:"published_at"`
	FeedID          string        `json:"feed_id"`
	FeedTitle       string        `json:"feed_title"`
	FeedContentKind string        `json:"feed_content_kind"`
	Confidence      string        `json:"confidence"`
	Evidence        []string      `json:"evidence"`
	Tracked         *trackedPaper `json:"tracked,omitempty"`
	WillImport      bool          `json:"will_import"`
}

type trackedPaper struct {
	PaperID string `json:"paper_id"`
	Kind    string `json:"kind"`
	Match   string `json:"match"`
}

type importOutcome struct {
	EntryID  string `json:"entry_id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	PaperID  string `json:"paper_id,omitempty"`
	FailedAt string `json:"failed_at,omitempty"`
	Error    string `json:"error,omitempty"`
	Code     string `json:"code,omitempty"`
}

func literatureCommands() []Command {
	return []Command{
		{
			Path:    "literature scan",
			Summary: "find papers inside subscribed feeds and match them against the workspace",
			Endpoint: "GET " + feedsPath + ", GET " + researchPapersPath + "?kind=, GET " + entriesPath +
				", then POST/PATCH " + researchPapersPath + " only with --import",
			Flags: []Flag{
				{Name: "kind", Kind: KindChoice, Choices: contentKinds, Default: "literature",
					Description: "only scan feeds in this category; --all-kinds scans every subscription"},
				{Name: "all-kinds", Kind: KindBool,
					Description: "scan general/video/social feeds too, which lowers precision"},
				{Name: "feed", Kind: KindString, Description: "limit the scan to one feed id"},
				{Name: "query", Kind: KindString, Description: "full-text search over entries"},
				{Name: "state", Kind: KindChoice, Choices: entryStates,
					Description: "unread limits the scan to what has not been read"},
				{Name: "since", Kind: KindString, Description: "time boundary, RFC3339 or YYYY-MM-DD"},
				{Name: "days", Kind: KindInt, Description: "shorthand for --since, N days back from now"},
				{Name: "max", Kind: KindInt, Default: "200", Description: "most entries to read across pages"},
				{Name: "min-confidence", Kind: KindChoice, Choices: confidenceTiers, Default: confidenceMedium,
					Description: "drop candidates below this tier from will_import"},
				{Name: "hide-tracked", Kind: KindBool,
					Description: "leave out entries already matched to a workspace paper"},
				{Name: "import", Kind: KindBool,
					Description: "write the will_import candidates into the workspace"},
				{Name: "target-kind", Kind: KindChoice, Choices: researchKinds,
					Default: domain.ResearchKindPublished,
					Description: "bucket to import into: published (已发表) carries DOI, journal and " +
						"citation fields, which is what feed literature is; use submitted (在投) only " +
						"for the user's own manuscripts"},
				{Name: "tag-id", Kind: KindStrings,
					Description: "research label ids to attach on import, repeatable"},
			},
			Handler: literatureScan,
		},
	}
}

func literatureScan(ctx context.Context, app *App, v *Values, args []string) error {
	if len(args) > 0 {
		return usage("literature scan takes no positional argument, got %q", args[0])
	}
	feeds, err := loadFeeds(ctx, app.client)
	if err != nil {
		return err
	}
	byDOI, byTitle, err := loadTracked(ctx, app.client)
	if err != nil {
		return err
	}
	entries, err := scanEntries(ctx, app.client, v, app.now)
	if err != nil {
		return err
	}
	minConfidence := v.String("min-confidence")
	if minConfidence == "" {
		minConfidence = confidenceMedium
	}
	target := v.String("target-kind")
	if target == "" {
		target = domain.ResearchKindPublished
	}
	out := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		item := classify(entry, feeds, byDOI, byTitle)
		// rank is best-first, so a candidate clears the floor when its rank is the
		// smaller number.
		if rank(item.Confidence) <= rank(minConfidence) && item.Tracked == nil {
			item.WillImport = true
		}
		if item.Tracked != nil && v.Bool("hide-tracked") {
			continue
		}
		out = append(out, item)
	}
	result := map[string]any{
		"scanned":        len(entries),
		"target_kind":    target,
		"min_confidence": minConfidence,
		"candidates":     out,
	}
	if v.Bool("import") {
		imported, err := importCandidates(ctx, app, out, target, v.Strings("tag-id"))
		if err != nil {
			return err
		}
		result["imported"] = imported
	}
	return app.Emit(result)
}

// classify turns one entry into a candidate, keeping the reason it qualified.
func classify(entry domain.Entry, feeds map[string]domain.Feed, byDOI, byTitle trackedIndex) candidate {
	feed := feeds[entry.FeedID]
	doi := ""
	if entry.DOI != nil {
		doi = normalizeDOI(*entry.DOI)
	}
	authors := splitAuthors(entry.Author)
	item := candidate{
		EntryID: entry.ID, Title: strings.TrimSpace(entry.Title), Authors: authors,
		DOI: doi, FeedID: entry.FeedID, FeedTitle: feed.Title,
		FeedContentKind: feed.ContentKind,
	}
	if entry.CanonicalURL != nil {
		item.CanonicalURL = *entry.CanonicalURL
	}
	item.PublishedAt = entry.PublishedAt.UTC().Format("2006-01-02")
	switch {
	case doi != "":
		item.Confidence = confidenceHigh
		item.Evidence = append(item.Evidence, "doi")
	case feed.ContentKind == domain.ContentKindLiterature && len(authors) > 0:
		item.Confidence = confidenceMedium
		item.Evidence = append(item.Evidence, "feed_is_literature", "authors")
	case feed.ContentKind == domain.ContentKindLiterature:
		item.Confidence = confidenceLow
		item.Evidence = append(item.Evidence, "feed_is_literature")
	default:
		item.Confidence = confidenceLow
		item.Evidence = append(item.Evidence, "title_only")
	}
	if match, ok := byDOI[doi]; ok && doi != "" {
		item.Tracked = &match
		item.Evidence = append(item.Evidence, "already_in_workspace")
	} else if match, ok := byTitle[normalizeTitle(entry.Title)]; ok {
		item.Tracked = &match
		item.Evidence = append(item.Evidence, "already_in_workspace")
	}
	return item
}

// importCandidates writes every will_import row, one paper at a time, and keeps
// going when the server rejects one: a single over-long title must not discard the
// other forty findings.
func importCandidates(ctx context.Context, app *App, items []candidate, kind string, tagIDs []string) ([]importOutcome, error) {
	out := make([]importOutcome, 0, len(items))
	for _, item := range items {
		if !item.WillImport {
			continue
		}
		body := map[string]any{"kind": kind, "title": item.Title}
		if len(item.Authors) > 0 {
			body["authors"] = item.Authors
		}
		created, err := app.client.Call(ctx, "POST", researchPapersPath, nil, body)
		if err != nil {
			out = append(out, failedImport(item, "create", err))
			continue
		}
		id, err := paperID(created)
		if err != nil {
			return nil, err
		}
		patch := map[string]any{
			"notes": fmt.Sprintf("由 reflow literature scan 从 RSS 条目 %s 导入：%s", item.EntryID, item.CanonicalURL),
		}
		if item.DOI != "" {
			patch["doi"] = item.DOI
		}
		if len(tagIDs) > 0 {
			patch["tag_ids"] = tagIDs
		}
		if _, err := app.client.Call(ctx, "PATCH", researchPapersPath+"/"+url.PathEscape(id), nil, patch); err != nil {
			// The row exists without its provenance note, so say which half failed.
			out = append(out, failedImport(item, "patch", err))
			out[len(out)-1].PaperID = id
			continue
		}
		out = append(out, importOutcome{EntryID: item.EntryID, Title: item.Title,
			Status: "created", PaperID: id})
	}
	return out, nil
}

func failedImport(item candidate, stage string, err error) importOutcome {
	out := importOutcome{EntryID: item.EntryID, Title: item.Title, Status: "failed", FailedAt: stage}
	var rejected *APIError
	if errors.As(err, &rejected) {
		out.Code = rejected.Code
		out.Error = rejected.Error()
	} else {
		out.Error = err.Error()
	}
	return out
}

// trackedIndex

type trackedIndex = map[string]trackedPaper

func loadTracked(ctx context.Context, client *Client) (trackedIndex, trackedIndex, error) {
	byDOI := trackedIndex{}
	byTitle := trackedIndex{}
	for _, kind := range researchKinds {
		var page struct {
			Items []domain.ResearchPaper `json:"items"`
		}
		if err := client.GetInto(ctx, researchPapersPath, url.Values{"kind": {kind}}, &page); err != nil {
			return nil, nil, err
		}
		for _, paper := range page.Items {
			if doi := normalizeDOI(paper.DOI); doi != "" {
				// The first row wins: two papers sharing a DOI is a duplicate the
				// user has to resolve in the table, not something the scanner picks.
				if _, seen := byDOI[doi]; !seen {
					byDOI[doi] = trackedPaper{PaperID: paper.ID, Kind: paper.Kind, Match: "doi"}
				}
			}
			key := normalizeTitle(paper.Title)
			if key != "" {
				if _, seen := byTitle[key]; !seen {
					byTitle[key] = trackedPaper{PaperID: paper.ID, Kind: paper.Kind, Match: "title"}
				}
			}
		}
	}
	return byDOI, byTitle, nil
}

func loadFeeds(ctx context.Context, client *Client) (map[string]domain.Feed, error) {
	var page struct {
		Items []domain.Feed `json:"items"`
	}
	if err := client.GetInto(ctx, feedsPath, nil, &page); err != nil {
		return nil, err
	}
	out := make(map[string]domain.Feed, len(page.Items))
	for _, feed := range page.Items {
		out[feed.ID] = feed
	}
	return out, nil
}

// scanEntries walks the timeline pages up to --max entries.
func scanEntries(ctx context.Context, client *Client, v *Values, now func() time.Time) ([]domain.Entry, error) {
	max := 200
	if v.Changed("max") {
		max = v.Int("max")
	}
	if max < 1 {
		return nil, usage("--max must be at least 1")
	}
	query, err := entryQuery(v, now)
	if err != nil {
		return nil, err
	}
	kind := v.String("kind")
	if kind != "" && v.Bool("all-kinds") {
		return nil, usage("pass either --kind %s or --all-kinds, not both", kind)
	}
	// The flag table is shared with `entry list`, whose page size is 30; a scan
	// wants the largest page the API allows so fewer round trips cover the window.
	query.Set("limit", "100")
	if v.Bool("all-kinds") {
		query.Del("content_kind")
	} else if kind == "" {
		query.Set("content_kind", domain.ContentKindLiterature)
	}
	collected := make([]domain.Entry, 0, min(max, 100))
	for {
		var page domain.EntryPage
		if err := client.GetInto(ctx, entriesPath, query, &page); err != nil {
			return nil, err
		}
		collected = append(collected, page.Items...)
		if page.NextCursor == nil || len(collected) >= max {
			break
		}
		query.Set("cursor", *page.NextCursor)
	}
	if len(collected) > max {
		collected = collected[:max]
	}
	sort.SliceStable(collected, func(i, j int) bool {
		return collected[i].PublishedAt.After(collected[j].PublishedAt)
	})
	return collected, nil
}

// DOI and title normalisation.

var (
	doiPrefixRE   = regexp.MustCompile(`(?i)^doi:\s*`)
	doiResolverRE = regexp.MustCompile(`(?i)^https?://(?:www\.|dx\.)?doi\.org/(?:doi/)?`)
	doiPlaceholdr = regexp.MustCompile(`(?i)^10\.x+$`)
	titleNoiseRE  = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// normalizeDOI matches the reader's own rule in web/src/lib/research.ts: strip a
// `doi:` label and a real resolver prefix, keep any other path segment intact, and
// drop the `10.xxxx/...` placeholders feeds ship with.
func normalizeDOI(raw string) string {
	value := strings.TrimSpace(raw)
	value = doiPrefixRE.ReplaceAllString(value, "")
	value = doiResolverRE.ReplaceAllString(value, "")
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	registrant, _, _ := strings.Cut(value, "/")
	if doiPlaceholdr.MatchString(strings.TrimSpace(registrant)) {
		return ""
	}
	return strings.ToLower(value)
}

// normalizeTitle collapses case and punctuation so "Effect of X, A Study" and
// "effect of x a study" are read as the same paper.
func normalizeTitle(raw string) string {
	collapsed := titleNoiseRE.ReplaceAllString(strings.TrimSpace(raw), " ")
	return strings.ToLower(strings.TrimSpace(collapsed))
}

// splitAuthors reads the single author string RSS hands over. Feeds separate names
// with either semicolons or commas, so both are cut; an author name that itself
// contains a comma is left split, which is what the reader shows too.
func splitAuthors(author *string) []string {
	if author == nil {
		return nil
	}
	raw := strings.TrimSpace(*author)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func rank(confidence string) int {
	for index, tier := range confidenceTiers {
		if tier == confidence {
			return index
		}
	}
	return len(confidenceTiers)
}
