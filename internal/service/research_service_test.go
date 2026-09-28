package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func TestNormalizeCrossrefDOI(t *testing.T) {
	cases := map[string]string{
		"https://doi.org/10.1000/xyz": "10.1000/xyz",
		"doi: 10.1000/xyz":            "10.1000/xyz",
		"10.1000/xyz":                 "10.1000/xyz",
		"10.xxxx/placeholder":         "",
		"":                            "",
		"not a doi":                   "",
	}
	for input, want := range cases {
		if got := NormalizeCrossrefDOI(input); got != want {
			t.Errorf("NormalizeCrossrefDOI(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsEnglishPaper(t *testing.T) {
	if IsEnglishPaper(domain.ResearchPaper{Journal: "经济研究"}) {
		t.Error("CJK journal should not be English")
	}
	if !IsEnglishPaper(domain.ResearchPaper{Journal: "American Economic Review"}) {
		t.Error("latin journal should be English")
	}
	if !IsEnglishPaper(domain.ResearchPaper{Language: "en"}) {
		t.Error("empty journal with en language should be English")
	}
}

func TestFetchCitationMapsCrossrefStatusCodes(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	paper, err := storage.CreateResearchPaper(ctx, db, domain.DefaultProfileID, domain.ResearchPaper{
		Kind: domain.ResearchKindPublished, Title: "P", Journal: "American Economic Review",
		DOI: "10.1000/missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	fetch := func(statusCode int) error {
		service := NewResearchService(db)
		service.client = stubCrossrefClient{statusCode: statusCode}
		_, err := service.FetchCitation(ctx, paper.ID)
		return err
	}
	if err := fetch(http.StatusNotFound); !errors.Is(err, ErrDoiNotFound) {
		t.Fatalf("404 should map to ErrDoiNotFound, got %v", err)
	}
	if err := fetch(http.StatusInternalServerError); !errors.Is(err, ErrCrossrefUnavailable) {
		t.Fatalf("5xx should map to ErrCrossrefUnavailable, got %v", err)
	}
}

type stubCrossrefClient struct{ statusCode int }

func (c stubCrossrefClient) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.statusCode,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     http.Header{},
	}, nil
}
