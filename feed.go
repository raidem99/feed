package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Episode is one downloadable item from a feed.
type Episode struct {
	GUID  string
	Title string
	URL   string // enclosure (audio) URL
}

// Minimal RSS structs. Go's encoding/xml maps tags to struct fields via tags
// (similar to a declarative schema in Python's pydantic).
type rss struct {
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			GUID      string `xml:"guid"`
			Title     string `xml:"title"`
			Enclosure struct {
				URL string `xml:"url,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

// ParseFeed reads RSS XML and returns the podcast title and its episodes
// (newest first, as feeds are normally ordered).
func ParseFeed(r io.Reader) (string, []Episode, error) {
	var doc rss
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return "", nil, fmt.Errorf("parse feed: %w", err)
	}
	var eps []Episode
	for _, it := range doc.Channel.Items {
		if it.Enclosure.URL == "" {
			continue // no audio, skip
		}
		id := it.GUID
		if id == "" {
			id = it.Enclosure.URL
		}
		eps = append(eps, Episode{GUID: id, Title: it.Title, URL: it.Enclosure.URL})
	}
	return doc.Channel.Title, eps, nil
}

// FetchFeed downloads and parses a feed URL.
func FetchFeed(client *http.Client, feedURL string) (string, []Episode, error) {
	resp, err := client.Get(feedURL)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("feed %s: %s", feedURL, resp.Status)
	}
	return ParseFeed(resp.Body)
}

var unsafeChars = regexp.MustCompile(`[^\w\-. ]+`)

// Sanitize makes a string safe to use as a file name.
func Sanitize(s string) string {
	s = strings.TrimSpace(unsafeChars.ReplaceAllString(s, "_"))
	if s == "" {
		return "untitled"
	}
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

// FileName builds "<title>.<ext>" using the extension from the audio URL.
func FileName(e Episode) string {
	ext := ".mp3"
	if u, err := url.Parse(e.URL); err == nil {
		if x := path.Ext(u.Path); x != "" && len(x) <= 5 {
			ext = x
		}
	}
	return Sanitize(e.Title) + ext
}
