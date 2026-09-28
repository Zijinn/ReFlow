package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/Zijinn/ReFlow/internal/domain"
	feedcore "github.com/Zijinn/ReFlow/internal/feed"
	"github.com/Zijinn/ReFlow/internal/opml"
	"github.com/Zijinn/ReFlow/internal/storage"
)

type AddFeedInput struct {
	URL           string
	FolderID      *string
	TitleOverride *string
}

func (s *FeedService) FetchReadability(ctx context.Context, entryID string) error {
	entry, err := storage.GetEntry(ctx, s.db, domain.DefaultProfileID, entryID)
	if err != nil {
		return err
	}
	if entry.CanonicalURL == nil {
		return errors.New("entry does not have a canonical URL")
	}
	fetched, err := s.fetcher.Fetch(ctx, *entry.CanonicalURL, feedcore.Validators{})
	if err != nil {
		return err
	}
	pageURL, err := url.Parse(fetched.URL)
	if err != nil {
		return fmt.Errorf("parse article URL: %w", err)
	}
	article, err := readability.FromReader(bytes.NewReader(fetched.Body), pageURL)
	if err != nil {
		return fmt.Errorf("extract readable article: %w", err)
	}
	var rendered bytes.Buffer
	if err := article.RenderHTML(&rendered); err != nil {
		return fmt.Errorf("render readable article: %w", err)
	}
	sanitized, plainText := s.parser.SanitizeHTML(rendered.String(), fetched.URL)
	if strings.TrimSpace(plainText) == "" {
		return errors.New("readability extraction returned no content")
	}
	return storage.SaveReadabilityContent(ctx, s.db, entryID, sanitized, plainText)
}

type ImportProgress func(current, total int)

type FeedService struct {
	db         *sql.DB
	fetcher    *feedcore.Fetcher
	parser     *feedcore.Parser
	discoverer *feedcore.Discoverer
	rssHubBase string
}

func NewFeedService(db *sql.DB, fetcher *feedcore.Fetcher) *FeedService {
	if fetcher == nil {
		fetcher = feedcore.NewFetcher()
	}
	parser := feedcore.NewParser()
	return &FeedService{
		db:         db,
		fetcher:    fetcher,
		parser:     parser,
		discoverer: feedcore.NewDiscoverer(fetcher, parser),
		rssHubBase: feedcore.DefaultRSSHubBase,
	}
}

func (s *FeedService) SetRSSHubBase(base string) {
	if strings.TrimSpace(base) != "" {
		s.rssHubBase = strings.TrimRight(strings.TrimSpace(base), "/")
	}
}

func (s *FeedService) AddFeed(ctx context.Context, input AddFeedInput) (domain.Feed, error) {
	target, err := feedcore.TransformRSSHubURL(input.URL, s.rssHubBase)
	if err != nil {
		return domain.Feed{}, err
	}
	fetched, err := s.fetcher.Fetch(ctx, target, feedcore.Validators{})
	if err != nil {
		return domain.Feed{}, err
	}
	parsed, err := s.parser.Parse(fetched.Body, fetched.URL)
	if err != nil {
		return domain.Feed{}, err
	}
	canonicalURL, err := feedcore.NormalizeURL(fetched.URL)
	if err != nil {
		return domain.Feed{}, err
	}
	stored, err := storage.SaveNewFeed(
		ctx, s.db, domain.DefaultProfileID, target, canonicalURL, parsed,
		fetched.ETag, fetched.LastModified, input.FolderID, input.TitleOverride,
	)
	if err != nil {
		return domain.Feed{}, err
	}
	if err := storage.ApplyRulesToFeed(ctx, s.db, domain.DefaultProfileID, stored.ID, nil); err != nil {
		return domain.Feed{}, err
	}
	return stored, nil
}

func (s *FeedService) Discover(ctx context.Context, rawURL string) ([]domain.FeedCandidate, error) {
	target, err := feedcore.TransformRSSHubURL(rawURL, s.rssHubBase)
	if err != nil {
		return nil, err
	}
	return s.discoverer.Discover(ctx, target)
}

func (s *FeedService) RefreshFeed(ctx context.Context, feedID string) (int, error) {
	stored, err := storage.GetFeed(ctx, s.db, feedID)
	if err != nil {
		return 0, err
	}
	fetched, err := s.fetcher.Fetch(ctx, stored.URL, feedcore.Validators{
		ETag:         stored.ETag,
		LastModified: stored.LastModified,
	})
	if err != nil {
		code := "refresh_error"
		var fetchError *feedcore.FetchError
		if errors.As(err, &fetchError) {
			code = fetchError.Code
		}
		_ = storage.MarkFeedFailure(ctx, s.db, feedID, code, err.Error())
		return 0, err
	}
	if fetched.NotModified {
		return 0, storage.MarkFeedNotModified(ctx, s.db, feedID)
	}
	parsed, err := s.parser.Parse(fetched.Body, fetched.URL)
	if err != nil {
		_ = storage.MarkFeedFailure(ctx, s.db, feedID, "parse_error", err.Error())
		return 0, err
	}
	inserted, err := storage.SaveFeedRefresh(ctx, s.db, domain.DefaultProfileID, feedID, parsed, fetched.ETag, fetched.LastModified)
	if err != nil {
		return 0, err
	}
	if err := storage.ApplyRulesToFeed(ctx, s.db, domain.DefaultProfileID, feedID, inserted); err != nil {
		return len(inserted), err
	}
	return len(inserted), nil
}

func (s *FeedService) ImportOPML(ctx context.Context, data []byte, progress ImportProgress) (int, error) {
	sources, err := opml.Parse(data)
	if err != nil {
		return 0, err
	}
	imported := 0
	var failures []error
	for index, source := range sources {
		var parentID *string
		for _, folderName := range source.FolderPath {
			folder, folderErr := storage.EnsureFolder(ctx, s.db, domain.DefaultProfileID, parentID, folderName)
			if folderErr != nil {
				failures = append(failures, fmt.Errorf("folder %q: %w", folderName, folderErr))
				parentID = nil
				break
			}
			folderID := folder.ID
			parentID = &folderID
		}
		if _, addErr := s.AddFeed(ctx, AddFeedInput{
			URL:           source.XMLURL,
			FolderID:      parentID,
			TitleOverride: stringPointer(source.Title),
		}); addErr != nil {
			failures = append(failures, fmt.Errorf("import %s: %w", source.XMLURL, addErr))
		} else {
			imported++
		}
		if progress != nil {
			progress(index+1, len(sources))
		}
	}
	return imported, errors.Join(failures...)
}

func (s *FeedService) ExportOPML(ctx context.Context) ([]byte, error) {
	feeds, err := storage.ListFeeds(ctx, s.db, domain.DefaultProfileID)
	if err != nil {
		return nil, err
	}
	subscriptions, err := storage.ListSubscriptions(ctx, s.db, domain.DefaultProfileID)
	if err != nil {
		return nil, err
	}
	folders, err := storage.ListFolders(ctx, s.db, domain.DefaultProfileID)
	if err != nil {
		return nil, err
	}
	feedByID := make(map[string]domain.Feed, len(feeds))
	for _, item := range feeds {
		feedByID[item.ID] = item
	}
	folderByID := make(map[string]domain.Folder, len(folders))
	for _, item := range folders {
		folderByID[item.ID] = item
	}
	sources := make([]opml.Source, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		stored, exists := feedByID[subscription.FeedID]
		if !exists {
			continue
		}
		sources = append(sources, opml.Source{
			Title:      subscription.Title,
			XMLURL:     stored.URL,
			HTMLURL:    valuePointer(stored.SiteURL),
			FolderPath: buildFolderPath(subscription.FolderID, folderByID),
		})
	}
	return opml.Export("ReFlow subscriptions", sources)
}

func buildFolderPath(folderID *string, folders map[string]domain.Folder) []string {
	if folderID == nil {
		return nil
	}
	path := make([]string, 0)
	seen := make(map[string]struct{})
	current := folderID
	for current != nil {
		if _, exists := seen[*current]; exists {
			break
		}
		seen[*current] = struct{}{}
		folder, exists := folders[*current]
		if !exists {
			break
		}
		path = append([]string{folder.Name}, path...)
		current = folder.ParentID
	}
	return path
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func valuePointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
