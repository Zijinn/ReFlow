package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	feedcore "github.com/Zijinn/ReFlow/internal/feed"
	"github.com/Zijinn/ReFlow/internal/storage"
	"github.com/google/uuid"
	"golang.org/x/net/html"
)

const zoteroBaseURL = "http://127.0.0.1:23119"

var (
	ErrZoteroUnavailable = errors.New("Zotero Desktop is not available on this computer")
	ErrZoteroTarget      = errors.New("select an editable Zotero library or collection first")
	doiPattern           = regexp.MustCompile(`(?i)\b10\.\d{4,9}/[-._;()/:A-Z0-9]+`)
)

type ZoteroStatus struct {
	Available      bool   `json:"available"`
	Editable       bool   `json:"editable"`
	LibraryID      string `json:"library_id,omitempty"`
	LibraryName    string `json:"library_name,omitempty"`
	CollectionID   string `json:"collection_id,omitempty"`
	CollectionName string `json:"collection_name,omitempty"`
}

type ZoteroSaveResult struct {
	Saved     bool                 `json:"saved"`
	Duplicate bool                 `json:"duplicate"`
	DOI       string               `json:"doi,omitempty"`
	Target    ZoteroStatus         `json:"target"`
	Export    storage.ZoteroExport `json:"export"`
}

type zoteroHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type ZoteroService struct {
	db      *sql.DB
	client  zoteroHTTPClient
	fetcher *feedcore.Fetcher
}

func NewZoteroService(db *sql.DB, fetcher *feedcore.Fetcher) *ZoteroService {
	if fetcher == nil {
		fetcher = feedcore.NewFetcher()
	}
	return &ZoteroService{
		db:      db,
		client:  &http.Client{Timeout: 4 * time.Second},
		fetcher: fetcher,
	}
}

func (s *ZoteroService) Status(ctx context.Context) (ZoteroStatus, error) {
	body, err := s.connector(ctx, "/connector/getSelectedCollection", []byte(`{}`), "application/json")
	if err != nil {
		return ZoteroStatus{Available: false}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ZoteroStatus{Available: false}, fmt.Errorf("decode Zotero target: %w", err)
	}
	status := ZoteroStatus{
		Available:      true,
		Editable:       boolValue(raw, "editable"),
		LibraryID:      stringValue(raw, "libraryID", "libraryId"),
		LibraryName:    stringValue(raw, "libraryName"),
		CollectionID:   stringValue(raw, "id", "collectionID", "collectionId"),
		CollectionName: stringValue(raw, "name", "collectionName"),
	}
	if status.LibraryName == "" {
		status.LibraryName = stringValue(raw, "library")
	}
	return status, nil
}

func (s *ZoteroService) GetExport(ctx context.Context, entryID string) (storage.ZoteroExport, error) {
	return storage.GetZoteroExport(ctx, s.db, domain.DefaultProfileID, entryID)
}

func (s *ZoteroService) Save(ctx context.Context, entryID string) (ZoteroSaveResult, error) {
	target, err := s.Status(ctx)
	if err != nil {
		return ZoteroSaveResult{}, err
	}
	if !target.Editable {
		return ZoteroSaveResult{}, ErrZoteroTarget
	}
	if existing, exportErr := storage.GetZoteroExport(ctx, s.db, domain.DefaultProfileID, entryID); exportErr == nil {
		return ZoteroSaveResult{Saved: true, Duplicate: true, Target: target, Export: existing}, nil
	} else if !errors.Is(exportErr, storage.ErrNotFound) {
		return ZoteroSaveResult{}, exportErr
	}
	entry, err := storage.GetZoteroEntry(ctx, s.db, domain.DefaultProfileID, entryID)
	if err != nil {
		return ZoteroSaveResult{}, err
	}
	metadata := metadataFromEntry(entry)
	if entry.CanonicalURL != "" {
		fetchCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		if page, fetchErr := s.fetcher.Fetch(fetchCtx, entry.CanonicalURL, feedcore.Validators{}); fetchErr == nil {
			metadata.merge(parseArticleMetadata(page.Body))
			if page.URL != "" {
				metadata.URL = page.URL
			}
		}
		cancel()
	}
	metadata.DOI = normalizeDOI(metadata.DOI)
	fingerprint := metadata.fingerprint()
	if existing, err := s.findDuplicate(ctx, metadata); err == nil && existing != "" {
		exported, saveErr := storage.SaveZoteroExport(ctx, s.db, domain.DefaultProfileID, storage.ZoteroExport{
			EntryID: entryID, ZoteroItemKey: existing, LibraryID: target.LibraryID,
			LibraryName: target.LibraryName, CollectionID: target.CollectionID,
			CollectionName: target.CollectionName, MetadataFingerprint: fingerprint,
		})
		if saveErr != nil {
			return ZoteroSaveResult{}, saveErr
		}
		return ZoteroSaveResult{Saved: true, Duplicate: true, DOI: metadata.DOI, Target: target, Export: exported}, nil
	} else if err != nil && !errors.Is(err, ErrZoteroUnavailable) {
		return ZoteroSaveResult{}, err
	}

	path := "/connector/import?session=" + url.QueryEscape("reflow-"+uuid.NewString())
	body, err := s.connector(ctx, path, []byte(metadata.ris()), "text/plain; charset=utf-8")
	if err != nil {
		return ZoteroSaveResult{}, err
	}
	itemKey := findItemKey(body)
	exported, err := storage.SaveZoteroExport(ctx, s.db, domain.DefaultProfileID, storage.ZoteroExport{
		EntryID: entryID, ZoteroItemKey: itemKey, LibraryID: target.LibraryID,
		LibraryName: target.LibraryName, CollectionID: target.CollectionID,
		CollectionName: target.CollectionName, MetadataFingerprint: fingerprint,
	})
	if err != nil {
		return ZoteroSaveResult{}, err
	}
	return ZoteroSaveResult{Saved: true, DOI: metadata.DOI, Target: target, Export: exported}, nil
}

func (s *ZoteroService) connector(ctx context.Context, path string, body []byte, contentType string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, zoteroBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", contentType)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrZoteroUnavailable, err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if readErr != nil {
		return nil, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Zotero Connector returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}

type articleMetadata struct {
	Title, DOI, URL, Journal, Date, Volume, Issue, FirstPage, LastPage, Abstract, Language, ISSN string
	Authors, Tags                                                                                []string
}

func metadataFromEntry(entry storage.ZoteroEntry) articleMetadata {
	date := ""
	if !entry.PublishedAt.IsZero() {
		date = entry.PublishedAt.Format("2006-01-02")
	}
	return articleMetadata{
		Title: entry.Title, DOI: entry.DOI, URL: entry.CanonicalURL, Journal: entry.Journal, Date: date,
		Abstract: entry.Summary, Language: entry.Language, Authors: storage.SplitZoteroAuthors(entry.Author),
		Tags: append([]string(nil), entry.Tags...),
	}
}

func (m *articleMetadata) merge(other articleMetadata) {
	if other.Title != "" {
		m.Title = other.Title
	}
	if other.DOI != "" {
		m.DOI = other.DOI
	}
	if other.Journal != "" {
		m.Journal = other.Journal
	}
	if other.Date != "" {
		m.Date = other.Date
	}
	if other.Volume != "" {
		m.Volume = other.Volume
	}
	if other.Issue != "" {
		m.Issue = other.Issue
	}
	if other.FirstPage != "" {
		m.FirstPage = other.FirstPage
	}
	if other.LastPage != "" {
		m.LastPage = other.LastPage
	}
	if other.Abstract != "" {
		m.Abstract = other.Abstract
	}
	if other.Language != "" {
		m.Language = other.Language
	}
	if other.ISSN != "" {
		m.ISSN = other.ISSN
	}
	if len(other.Authors) > 0 {
		m.Authors = other.Authors
	}
}

func parseArticleMetadata(body []byte) articleMetadata {
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return articleMetadata{}
	}
	values := make(map[string][]string)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "meta" {
			name, content := "", ""
			for _, attribute := range node.Attr {
				switch strings.ToLower(attribute.Key) {
				case "name", "property":
					name = strings.ToLower(strings.TrimSpace(attribute.Val))
				case "content":
					content = strings.TrimSpace(attribute.Val)
				}
			}
			if name != "" && content != "" {
				values[name] = append(values[name], content)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	first := func(keys ...string) string {
		for _, key := range keys {
			if list := values[key]; len(list) > 0 {
				return list[0]
			}
		}
		return ""
	}
	doi := first("citation_doi", "prism.doi", "dc.identifier", "dc.identifier.doi")
	if match := doiPattern.FindString(doi); match != "" {
		doi = match
	}
	return articleMetadata{
		Title: first("citation_title", "dc.title", "og:title"), DOI: doi,
		Journal: first("citation_journal_title", "prism.publicationname"),
		Date:    first("citation_publication_date", "citation_date", "prism.publicationdate", "dc.date"),
		Volume:  first("citation_volume", "prism.volume"), Issue: first("citation_issue", "prism.number"),
		FirstPage: first("citation_firstpage", "prism.startingpage"), LastPage: first("citation_lastpage", "prism.endingpage"),
		Abstract: first("citation_abstract", "dc.description", "description"),
		Language: first("citation_language", "dc.language"), ISSN: first("citation_issn", "prism.issn"),
		Authors: append(append([]string(nil), values["citation_author"]...), values["dc.creator"]...),
	}
}

func (m articleMetadata) ris() string {
	lines := []string{"TY  - JOUR", "TI  - " + risValue(m.Title)}
	for _, author := range m.Authors {
		if author = risValue(author); author != "" {
			lines = append(lines, "AU  - "+author)
		}
	}
	fields := [][2]string{{"JO", m.Journal}, {"PY", m.Date}, {"VL", m.Volume}, {"IS", m.Issue}, {"SP", m.FirstPage}, {"EP", m.LastPage}, {"AB", m.Abstract}, {"UR", m.URL}, {"DO", m.DOI}, {"LA", m.Language}, {"SN", m.ISSN}}
	for _, field := range fields {
		if value := risValue(field[1]); value != "" {
			lines = append(lines, field[0]+"  - "+value)
		}
	}
	for _, tag := range m.Tags {
		if tag = risValue(tag); tag != "" {
			lines = append(lines, "KW  - "+tag)
		}
	}
	return strings.Join(append(lines, "ER  - ", ""), "\r\n")
}

func (m articleMetadata) fingerprint() string {
	values := []string{m.Title, strings.Join(m.Authors, ";"), m.Journal, m.Date, m.URL, m.DOI}
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func (s *ZoteroService) findDuplicate(ctx context.Context, metadata articleMetadata) (string, error) {
	query := metadata.DOI
	if query == "" {
		query = metadata.Title
	}
	if strings.TrimSpace(query) == "" {
		return "", nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, zoteroBaseURL+"/api/users/0/items/top?q="+url.QueryEscape(query)+"&limit=50", nil)
	if err != nil {
		return "", err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrZoteroUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Zotero local API returned HTTP %d", response.StatusCode)
	}
	var items []struct {
		Key  string                                                   `json:"key"`
		Data struct{ DOI, URL, Title, Date, PublicationTitle string } `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&items); err != nil {
		return "", err
	}
	for _, item := range items {
		if metadata.DOI != "" && normalizeDOI(item.Data.DOI) == metadata.DOI {
			return item.Key, nil
		}
		if metadata.URL != "" && sameCanonicalURL(item.Data.URL, metadata.URL) {
			return item.Key, nil
		}
		if normalizedText(item.Data.Title) == normalizedText(metadata.Title) && year(item.Data.Date) == year(metadata.Date) && normalizedText(item.Data.PublicationTitle) == normalizedText(metadata.Journal) {
			return item.Key, nil
		}
	}
	return "", nil
}

func normalizeDOI(value string) string {
	value = strings.TrimSpace(value)
	if match := doiPattern.FindString(value); match != "" {
		value = match
	}
	value = strings.TrimRight(value, ".,;:)]}")
	if !doiPattern.MatchString(value) {
		return ""
	}
	return strings.ToLower(value)
}

func sameCanonicalURL(left, right string) bool {
	a, errA := feedcore.NormalizeURL(left)
	b, errB := feedcore.NormalizeURL(right)
	return errA == nil && errB == nil && a == b
}

func normalizedText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
func year(value string) string {
	if match := regexp.MustCompile(`\b\d{4}\b`).FindString(value); match != "" {
		return match
	}
	return ""
}
func risValue(value string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(value))
}

func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		switch raw := value[key].(type) {
		case string:
			return raw
		case float64:
			return fmt.Sprintf("%.0f", raw)
		}
	}
	return ""
}
func boolValue(value map[string]any, key string) bool { result, _ := value[key].(bool); return result }

func findItemKey(body []byte) string {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return ""
	}
	keys := make([]string, 0)
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if key == "key" || key == "itemKey" {
					if text, ok := nested.(string); ok && text != "" {
						keys = append(keys, text)
					}
				}
				walk(nested)
			}
		case []any:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	walk(value)
	sort.Strings(keys)
	if len(keys) > 0 {
		return keys[0]
	}
	return ""
}
