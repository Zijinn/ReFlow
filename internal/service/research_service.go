package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

var (
	// ErrCrossrefUnavailable is returned when Crossref cannot be reached or
	// fails with a retryable error.
	ErrCrossrefUnavailable = errors.New("Crossref is not reachable right now")
	// ErrDoiNotFound is returned when Crossref reports no work for the DOI.
	ErrDoiNotFound = errors.New("Crossref found no work for this DOI")
	// ErrCitationNotChinese is returned when a Crossref lookup is requested for
	// a Chinese-journal paper, which Crossref does not index by DOI here.
	ErrCitationNotEnglish = errors.New("Crossref lookup only applies to English-journal papers")
	// ErrCitationNoDOI is returned when the paper lacks a usable DOI.
	ErrCitationNoDOI = errors.New("a real DOI is required to query Crossref")

	crossrefDOIPattern = regexp.MustCompile(`(?i)\b10\.\d{4,9}/[-._;()/:A-Z0-9]+`)
	cjkPattern         = regexp.MustCompile(`[\x{4e00}-\x{9fff}]`)
)

type researchHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// ResearchService owns the research workspace domain logic: Crossref citation
// lookups and reference formatting that must not live in the transport layer.
type ResearchService struct {
	db     *sql.DB
	client researchHTTPClient
}

// NewResearchService builds a research service with a bounded HTTP client.
func NewResearchService(db *sql.DB) *ResearchService {
	return &ResearchService{
		db:     db,
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

// FetchCitation queries Crossref for the current citation count of an
// English-journal paper and persists the result. Chinese papers and papers
// without a real DOI are rejected so callers can prompt for manual entry.
func (s *ResearchService) FetchCitation(ctx context.Context, id string) (domain.ResearchPaper, error) {
	paper, err := storage.GetResearchPaper(ctx, s.db, domain.DefaultProfileID, id)
	if err != nil {
		return domain.ResearchPaper{}, err
	}
	if !IsEnglishPaper(paper) {
		return domain.ResearchPaper{}, ErrCitationNotEnglish
	}
	doi := NormalizeCrossrefDOI(paper.DOI)
	if doi == "" {
		return domain.ResearchPaper{}, ErrCitationNoDOI
	}
	email, err := storage.CrossrefEmail(ctx, s.db, domain.DefaultProfileID)
	if err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("read Crossref contact email: %w", err)
	}
	endpoint := "https://api.crossref.org/v1/works/" + url.PathEscape(doi)
	if email != "" {
		endpoint += "?mailto=" + url.QueryEscape(email)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return domain.ResearchPaper{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "ReFlow/1.0 (research workspace)")
	response, err := s.client.Do(request)
	if err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("%w: %v", ErrCrossrefUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return domain.ResearchPaper{}, fmt.Errorf("%w: %s", ErrDoiNotFound, doi)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.ResearchPaper{}, fmt.Errorf("%w: Crossref returned HTTP %d", ErrCrossrefUnavailable, response.StatusCode)
	}
	var payload struct {
		Message struct {
			Count int `json:"is-referenced-by-count"`
		} `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return domain.ResearchPaper{}, fmt.Errorf("%w: decode Crossref response", ErrCrossrefUnavailable)
	}
	count := payload.Message.Count
	source := "Crossref"
	return storage.UpdateResearchPaper(ctx, s.db, domain.DefaultProfileID, id, domain.ResearchPaperPatch{
		SetCitations: true, Citations: &count, CitationSource: &source,
	})
}

// IsEnglishPaper mirrors the original dashboard rule: a paper is English when
// its journal name has no CJK characters (falling back to the language field
// when the journal is empty).
func IsEnglishPaper(paper domain.ResearchPaper) bool {
	journal := strings.TrimSpace(paper.Journal)
	if journal == "" {
		return strings.EqualFold(strings.TrimSpace(paper.Language), "en")
	}
	return !cjkPattern.MatchString(journal)
}

// NormalizeCrossrefDOI strips URL/prefix noise and rejects placeholder DOIs.
func NormalizeCrossrefDOI(raw string) string {
	value := strings.TrimSpace(raw)
	value = regexp.MustCompile(`(?i)^https?://(doi\.org/)?`).ReplaceAllString(value, "")
	value = regexp.MustCompile(`(?i)^doi:\s*`).ReplaceAllString(value, "")
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(strings.ToLower(value), "xxxx") {
		return ""
	}
	if match := crossrefDOIPattern.FindString(value); match != "" {
		return match
	}
	return ""
}
