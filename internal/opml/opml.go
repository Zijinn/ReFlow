package opml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

type Source struct {
	Title      string
	XMLURL     string
	HTMLURL    string
	FolderPath []string
}

type document struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    head     `xml:"head"`
	Body    body     `xml:"body"`
}

type head struct {
	Title       string `xml:"title"`
	DateCreated string `xml:"dateCreated,omitempty"`
}

type body struct {
	Outlines []outline `xml:"outline"`
}

type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Children []outline `xml:"outline,omitempty"`
}

const (
	maxOutlineDepth   = 32
	maxOutlineSources = 10000
)

func Parse(data []byte) ([]Source, error) {
	// Some desktop RSS tools prepend a UTF-8 BOM. encoding/xml does not
	// consistently accept it when the document is sent as a raw upload.
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("parse OPML: document is empty")
	}
	var parsed document
	if err := xml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse OPML: %w", err)
	}
	if !strings.EqualFold(parsed.XMLName.Local, "opml") {
		return nil, fmt.Errorf("parse OPML: root element is not opml")
	}
	result := make([]Source, 0)
	var flatten func([]outline, []string, int) error
	flatten = func(items []outline, path []string, depth int) error {
		if depth > maxOutlineDepth {
			return fmt.Errorf("parse OPML: outline nesting exceeds %d levels", maxOutlineDepth)
		}
		for _, item := range items {
			if len(result) >= maxOutlineSources {
				return fmt.Errorf("parse OPML: more than %d subscriptions", maxOutlineSources)
			}
			title := strings.TrimSpace(item.Title)
			if title == "" {
				title = strings.TrimSpace(item.Text)
			}
			if strings.TrimSpace(item.XMLURL) != "" {
				result = append(result, Source{
					Title:      title,
					XMLURL:     strings.TrimSpace(item.XMLURL),
					HTMLURL:    strings.TrimSpace(item.HTMLURL),
					FolderPath: append([]string(nil), path...),
				})
				continue
			}
			nextPath := path
			if title != "" {
				nextPath = append(append([]string(nil), path...), title)
			}
			if err := flatten(item.Children, nextPath, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := flatten(parsed.Body.Outlines, nil, 0); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("parse OPML: no RSS subscriptions found")
	}
	return result, nil
}

func Export(title string, sources []Source) ([]byte, error) {
	root := outline{Children: make([]outline, 0)}
	for _, source := range sources {
		container := &root
		for _, folder := range source.FolderPath {
			folder = strings.TrimSpace(folder)
			if folder == "" {
				continue
			}
			index := -1
			for candidate := range container.Children {
				child := &container.Children[candidate]
				if child.XMLURL == "" && child.Text == folder {
					index = candidate
					break
				}
			}
			if index == -1 {
				container.Children = append(container.Children, outline{Text: folder, Title: folder})
				index = len(container.Children) - 1
			}
			container = &container.Children[index]
		}
		sourceTitle := strings.TrimSpace(source.Title)
		if sourceTitle == "" {
			sourceTitle = source.XMLURL
		}
		container.Children = append(container.Children, outline{
			Text: sourceTitle, Title: sourceTitle, Type: "rss",
			XMLURL: source.XMLURL, HTMLURL: source.HTMLURL,
		})
	}
	parsed := document{
		Version: "2.0",
		Head:    head{Title: title, DateCreated: time.Now().UTC().Format(time.RFC1123Z)},
		Body:    body{Outlines: root.Children},
	}
	body, err := xml.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode OPML: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}
