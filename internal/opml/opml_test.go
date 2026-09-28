package opml

import (
	"reflect"
	"testing"
)

func TestParseAcceptsBOMAndNestedOutlines(t *testing.T) {
	body := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<?xml version="1.0"?>
<opml version="2.0"><head><title>中文期刊</title></head><body>
  <outline text="A1"><outline text="经济研究" type="rss" xmlUrl="https://example.com/rss?a=1&amp;b=2" /></outline>
</body></opml>`)...)
	sources, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Title != "经济研究" || sources[0].FolderPath[0] != "A1" {
		t.Fatalf("unexpected sources: %#v", sources)
	}
	if sources[0].XMLURL != "https://example.com/rss?a=1&b=2" {
		t.Fatalf("unexpected URL: %q", sources[0].XMLURL)
	}
}

func TestParseRejectsEmptySubscriptionList(t *testing.T) {
	if _, err := Parse([]byte(`<opml version="2.0"><body><outline text="Empty" /></body></opml>`)); err == nil {
		t.Fatal("expected an empty OPML document to be rejected")
	}
}

func TestRoundTripPreservesFoldersAndURLs(t *testing.T) {
	want := []Source{
		{Title: "One", XMLURL: "https://example.com/one.xml", HTMLURL: "https://example.com/one", FolderPath: []string{"Research", "Economics"}},
		{Title: "Two", XMLURL: "https://example.com/two.json", FolderPath: []string{"Research"}},
	}
	body, err := Export("ReFlow subscriptions", want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\nwant: %#v\n got: %#v", want, got)
	}
}
