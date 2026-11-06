package main

import (
	"strings"
	"testing"
)

const sample = `<?xml version="1.0"?>
<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"
     xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel><title>My Show</title>
<itunes:author>Jane Doe</itunes:author>
<itunes:image href="https://x.com/cover.jpg"/>
<item><guid>a</guid><title>Ep 2: Hi/there?</title>
  <description>short</description>
  <content:encoded><![CDATA[<p>long notes</p>]]></content:encoded>
  <enclosure url="https://x.com/2.mp3?t=1"/></item>
<item><title>No audio</title></item>
<item><title>Ep 1</title><description>plain</description><enclosure url="https://x.com/1.m4a"/></item>
</channel></rss>`

func TestParseFeed(t *testing.T) {
	f, err := ParseFeed(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "My Show" || f.Author != "Jane Doe" {
		t.Errorf("title/author = %q / %q", f.Title, f.Author)
	}
	if f.ImageURL != "https://x.com/cover.jpg" {
		t.Errorf("image = %q", f.ImageURL)
	}
	eps := f.Episodes
	if len(eps) != 2 {
		t.Fatalf("got %d episodes, want 2", len(eps))
	}
	if eps[0].GUID != "a" {
		t.Errorf("guid = %q", eps[0].GUID)
	}
	if eps[0].Notes != "<p>long notes</p>" { // content:encoded wins
		t.Errorf("notes = %q", eps[0].Notes)
	}
	if eps[1].Notes != "plain" { // falls back to description
		t.Errorf("fallback notes = %q", eps[1].Notes)
	}
	if eps[1].GUID != "https://x.com/1.m4a" { // falls back to URL
		t.Errorf("fallback guid = %q", eps[1].GUID)
	}
}

func TestParseFeedPlainImage(t *testing.T) {
	f, err := ParseFeed(strings.NewReader(
		`<rss><channel><title>T</title><image><url>https://x.com/i.png</url></image></channel></rss>`))
	if err != nil {
		t.Fatal(err)
	}
	if f.ImageURL != "https://x.com/i.png" {
		t.Errorf("image = %q", f.ImageURL)
	}
}

func TestParseFeedInvalid(t *testing.T) {
	if _, err := ParseFeed(strings.NewReader("not xml")); err == nil {
		t.Error("expected error")
	}
}

func TestFileName(t *testing.T) {
	cases := []struct {
		ep   Episode
		want string
	}{
		{Episode{Title: "Ep 2: Hi/there?", URL: "https://x.com/2.mp3?t=1"}, "Ep 2_ Hi_there_.mp3"},
		{Episode{Title: "Ep 1", URL: "https://x.com/1.m4a"}, "Ep 1.m4a"},
		{Episode{Title: "", URL: "https://x.com/audio"}, "untitled.mp3"},
	}
	for _, c := range cases {
		if got := FileName(c.ep); got != c.want {
			t.Errorf("FileName(%q) = %q, want %q", c.ep.Title, got, c.want)
		}
	}
}

func TestReadFeedList(t *testing.T) {
	in := "# my feeds\nhttps://a.com/feed\n\n  https://b.com/rss  # trailing\n#https://off\n"
	got, err := ReadFeedList(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://a.com/feed", "https://b.com/rss"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %q, want %q", got, want)
	}
}
