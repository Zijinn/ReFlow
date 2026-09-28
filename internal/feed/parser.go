package feed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
)

type Parser struct {
	sanitizer *bluemonday.Policy
}

func NewParser() *Parser {
	policy := bluemonday.UGCPolicy()
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	policy.AllowURLSchemes("http", "https")
	policy.AllowElements("audio", "video", "source", "figure", "figcaption")
	policy.AllowAttrs("src", "type").OnElements("source")
	policy.AllowAttrs("src", "controls", "preload").OnElements("audio")
	policy.AllowAttrs("src", "controls", "preload", "poster").OnElements("video")
	policy.AllowElements("math", "semantics", "mrow", "mi", "mo", "mn", "msup", "msub", "mfrac", "msqrt", "mroot", "mtext", "annotation")
	policy.AllowAttrs("display").OnElements("math")
	policy.AllowAttrs("encoding").OnElements("annotation")
	return &Parser{sanitizer: policy}
}

func (p *Parser) Parse(body []byte, sourceURL string) (domain.ParsedFeed, error) {
	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return domain.ParsedFeed{}, fmt.Errorf("parse feed: %w", err)
	}

	siteURL := ResolveURL(sourceURL, parsed.Link)
	iconCandidate := ""
	if parsed.Image != nil {
		iconCandidate = parsed.Image.URL
	}
	iconURL := ResolveURL(valuePointer(siteURL), iconCandidate)
	result := domain.ParsedFeed{
		Title:       strings.TrimSpace(parsed.Title),
		Description: stringValuePointer(strings.TrimSpace(parsed.Description)),
		SiteURL:     siteURL,
		IconURL:     iconURL,
		Format:      feedFormat(fmt.Sprint(parsed.FeedType)),
		Entries:     make([]domain.ParsedEntry, 0, len(parsed.Items)),
	}
	if result.Title == "" {
		if source, parseErr := url.Parse(sourceURL); parseErr == nil {
			result.Title = source.Hostname()
		}
	}

	baseURL := valuePointer(siteURL)
	if baseURL == "" {
		baseURL = sourceURL
	}
	now := time.Now().UTC()
	for _, item := range parsed.Items {
		entry := normalizeItem(p, item, baseURL, now)
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func normalizeItem(parser *Parser, item *gofeed.Item, baseURL string, fallbackTime time.Time) domain.ParsedEntry {
	canonicalURL := ResolveURL(baseURL, item.Link)
	content := item.Content
	if strings.TrimSpace(content) == "" {
		content = item.Description
	}
	sanitized, plainText, firstImage := parser.sanitize(content, valuePointer(canonicalURL))

	publishedAt := fallbackTime
	hasPublishedAt := false
	if item.PublishedParsed != nil {
		publishedAt = item.PublishedParsed.UTC()
		hasPublishedAt = true
	} else if item.UpdatedParsed != nil {
		publishedAt = item.UpdatedParsed.UTC()
		hasPublishedAt = true
	}
	author := ""
	if item.Author != nil {
		author = strings.TrimSpace(item.Author.Name)
	}
	leadImage := firstImage
	if item.Image != nil {
		leadImage = ResolveURL(baseURL, item.Image.URL)
	}
	var audioURL, videoURL *string
	for _, enclosure := range item.Enclosures {
		mediaURL := ResolveURL(baseURL, enclosure.URL)
		if mediaURL == nil {
			continue
		}
		mediaType := strings.ToLower(enclosure.Type)
		switch {
		case audioURL == nil && strings.HasPrefix(mediaType, "audio/"):
			audioURL = mediaURL
		case videoURL == nil && strings.HasPrefix(mediaType, "video/"):
			videoURL = mediaURL
		}
	}

	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = truncateText(plainText, 100)
	}
	summary := truncateText(plainText, 360)
	hashInput := strings.Join([]string{
		strings.TrimSpace(item.GUID),
		valuePointer(canonicalURL),
		title,
		plainText,
	}, "\x00")
	digest := sha256.Sum256([]byte(hashInput))
	var identityPublishedAt *time.Time
	if hasPublishedAt {
		identityPublishedAt = &publishedAt
	}
	identityHash := ComputeIdentityHash(title, author, identityPublishedAt, plainText)
	doi := feedItemDOI(item)

	return domain.ParsedEntry{
		GUID:          stringValuePointer(strings.TrimSpace(item.GUID)),
		CanonicalURL:  canonicalURL,
		Title:         title,
		Author:        stringValuePointer(author),
		Summary:       stringValuePointer(summary),
		PublishedAt:   publishedAt,
		ContentHash:   hex.EncodeToString(digest[:]),
		IdentityHash:  identityHash,
		SourceHTML:    content,
		SanitizedHTML: sanitized,
		PlainText:     plainText,
		LeadImageURL:  leadImage,
		AudioURL:      audioURL,
		VideoURL:      videoURL,
		DOI:           stringValuePointer(doi),
	}
}

func feedItemDOI(item *gofeed.Item) string {
	candidates := make([]string, 0, 6)
	if item.DublinCoreExt != nil {
		candidates = append(candidates, item.DublinCoreExt.Identifier...)
	}
	for namespace, fields := range item.Extensions {
		for name, values := range fields {
			qualified := strings.ToLower(namespace + "." + name)
			if name != "doi" && qualified != "dc.identifier" && qualified != "prism.doi" {
				continue
			}
			for _, value := range values {
				candidates = append(candidates, value.Value)
			}
		}
	}
	for _, candidate := range candidates {
		if doi := normalizeFeedDOI(candidate); doi != "" {
			return doi
		}
	}
	return ""
}

func normalizeFeedDOI(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "doi:"} {
		if index := strings.Index(lower, prefix); index >= 0 {
			value = value[index+len(prefix):]
			lower = strings.ToLower(value)
		}
	}
	if !strings.HasPrefix(lower, "10.") || !strings.Contains(value, "/") {
		return ""
	}
	return strings.ToLower(strings.TrimRight(value, ".,;:)]}"))
}

func ComputeIdentityHash(title, author string, publishedAt *time.Time, plainText string) string {
	identityParts := []string{
		strings.ToLower(collapseWhitespace(title)),
		strings.ToLower(collapseWhitespace(author)),
	}
	if publishedAt != nil {
		identityParts = append(identityParts, publishedAt.UTC().Format(time.RFC3339))
	}
	// A short body prefix reduces false matches for feeds without stable IDs while
	// remaining stable when publishers append or edit the end of an article.
	identityParts = append(identityParts, strings.ToLower(truncateText(plainText, 128)))
	digest := sha256.Sum256([]byte(strings.Join(identityParts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func (p *Parser) sanitize(rawHTML, baseURL string) (string, string, *string) {
	rewritten := rewriteResourceURLs(rawHTML, baseURL)
	sanitized := p.sanitizer.Sanitize(rewritten)
	document, err := html.Parse(strings.NewReader(sanitized))
	if err != nil {
		return sanitized, strings.TrimSpace(html.UnescapeString(sanitized)), nil
	}
	var text strings.Builder
	var leadImage *string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
			text.WriteByte(' ')
		}
		if node.Type == html.ElementNode && node.Data == "img" && leadImage == nil {
			for _, attribute := range node.Attr {
				if attribute.Key == "src" {
					leadImage = stringValuePointer(attribute.Val)
					break
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return sanitized, collapseWhitespace(text.String()), leadImage
}

func (p *Parser) SanitizeHTML(rawHTML, baseURL string) (string, string) {
	sanitized, plainText, _ := p.sanitize(rawHTML, baseURL)
	return sanitized, plainText
}

func rewriteResourceURLs(rawHTML, baseURL string) string {
	if strings.TrimSpace(rawHTML) == "" {
		return ""
	}
	document, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for index := range node.Attr {
				key := strings.ToLower(node.Attr[index].Key)
				if key != "href" && key != "src" && key != "poster" {
					continue
				}
				resolved := ResolveURL(baseURL, node.Attr[index].Val)
				if resolved == nil {
					node.Attr[index].Val = ""
				} else {
					node.Attr[index].Val = *resolved
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)

	var output strings.Builder
	body := findElement(document, "body")
	if body == nil {
		body = document
	}
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		_ = html.Render(&output, child)
	}
	return output.String()
}

func findElement(node *html.Node, name string) *html.Node {
	if node.Type == html.ElementNode && node.Data == name {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func feedFormat(value string) string {
	value = strings.ToLower(value)
	switch {
	case strings.Contains(value, "atom"):
		return "atom"
	case strings.Contains(value, "json"):
		return "json"
	default:
		return "rss"
	}
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
}

func truncateText(value string, limit int) string {
	value = collapseWhitespace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func stringValuePointer(value string) *string {
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
