package feed

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Zijinn/ReFlow/internal/domain"
	"golang.org/x/net/html"
)

type Discoverer struct {
	Fetcher *Fetcher
	Parser  *Parser
}

func NewDiscoverer(fetcher *Fetcher, parser *Parser) *Discoverer {
	return &Discoverer{Fetcher: fetcher, Parser: parser}
}

func (d *Discoverer) Discover(ctx context.Context, rawURL string) ([]domain.FeedCandidate, error) {
	result, err := d.Fetcher.Fetch(ctx, rawURL, Validators{})
	if err != nil {
		return nil, err
	}
	if parsed, parseErr := d.Parser.Parse(result.Body, result.URL); parseErr == nil {
		return []domain.FeedCandidate{{URL: result.URL, Title: parsed.Title, SiteURL: parsed.SiteURL}}, nil
	}

	document, err := html.Parse(strings.NewReader(string(result.Body)))
	if err != nil {
		return nil, fmt.Errorf("parse discovery page: %w", err)
	}
	pageTitle := ""
	if titleNode := findElement(document, "title"); titleNode != nil && titleNode.FirstChild != nil {
		pageTitle = collapseWhitespace(titleNode.FirstChild.Data)
	}
	base, _ := url.Parse(result.URL)
	seen := make(map[string]struct{})
	candidates := make([]domain.FeedCandidate, 0)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "link" {
			attributes := make(map[string]string, len(node.Attr))
			for _, attribute := range node.Attr {
				attributes[strings.ToLower(attribute.Key)] = attribute.Val
			}
			if hasRel(attributes["rel"], "alternate") && isFeedMediaType(attributes["type"]) {
				reference, parseErr := url.Parse(strings.TrimSpace(attributes["href"]))
				if parseErr == nil && reference.String() != "" {
					resolved := base.ResolveReference(reference)
					normalized, normalizeErr := NormalizeURL(resolved.String())
					if normalizeErr == nil {
						if _, exists := seen[normalized]; !exists {
							seen[normalized] = struct{}{}
							title := collapseWhitespace(attributes["title"])
							if title == "" {
								title = pageTitle
							}
							candidates = append(candidates, domain.FeedCandidate{URL: normalized, Title: title, SiteURL: &result.URL})
						}
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no RSS, Atom, or JSON Feed links found")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].URL < candidates[j].URL })
	return candidates, nil
}

func hasRel(value, wanted string) bool {
	for _, token := range strings.Fields(strings.ToLower(value)) {
		if token == wanted {
			return true
		}
	}
	return false
}

func isFeedMediaType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	switch value {
	case "application/rss+xml", "application/atom+xml", "application/feed+json", "application/json":
		return true
	default:
		return false
	}
}
