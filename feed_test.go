package main

import (
	"strings"
	"testing"
)

const sample = `<?xml version="1.0"?>
<rss><channel><title>My Show</title>
<item><guid>a</guid><title>Ep 2: Hi/there?</title><enclosure url="https://x.com/2.mp3?t=1"/></item>
<item><title>No audio</title></item>
<item><title>Ep 1</title><enclosure url="https://x.com/1.m4a"/></item>
</channel></rss>`

func TestParseFeed(t *testing.T) {
	title, eps, err := ParseFeed(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if title != "My Show" {
		t.Errorf("title = %q", title)
	}
	if len(eps) != 2 {
		t.Fatalf("got %d episodes, want 2", len(eps))
	}
	if eps[0].GUID != "a" {
		t.Errorf("guid = %q", eps[0].GUID)
	}
	if eps[1].GUID != "https://x.com/1.m4a" { // falls back to URL
		t.Errorf("fallback guid = %q", eps[1].GUID)
	}
}

func TestParseFeedInvalid(t *testing.T) {
	if _, _, err := ParseFeed(strings.NewReader("not xml")); err == nil {
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
