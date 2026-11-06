package main

import (
	"bufio"
	"context"
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
	Notes string // show notes (HTML or plain text), may be empty
}

// Feed is a parsed podcast feed.
type Feed struct {
	Title    string
	Author   string
	ImageURL string
	Episodes []Episode // newest first, as feeds are normally ordered
}

// Minimal RSS structs. Go's encoding/xml maps tags to struct fields via tags
// (similar to a declarative schema in Python's pydantic). A tag without a
// namespace matches the local name in ANY namespace, so `author` catches
// <itunes:author> and `encoded` catches <content:encoded>.
type rss struct {
	Channel struct {
		Title  string `xml:"title"`
		Author string `xml:"author"`
		// Both <itunes:image href="..."/> and <image><url>..</url></image>.
		Images []struct {
			Href string `xml:"href,attr"`
			URL  string `xml:"url"`
		} `xml:"image"`
		Items []struct {
			GUID        string `xml:"guid"`
			Title       string `xml:"title"`
			Description string `xml:"description"`
			Encoded     string `xml:"encoded"`
			Enclosure   struct {
				URL string `xml:"url,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

// ParseFeed reads RSS XML into a Feed.
func ParseFeed(r io.Reader) (Feed, error) {
	var doc rss
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return Feed{}, fmt.Errorf("parse feed: %w", err)
	}
	f := Feed{
		Title:  strings.TrimSpace(doc.Channel.Title),
		Author: strings.TrimSpace(doc.Channel.Author),
	}
	for _, img := range doc.Channel.Images {
		if u := strings.TrimSpace(img.Href); u != "" {
			f.ImageURL = u
			break
		}
		if u := strings.TrimSpace(img.URL); u != "" {
			f.ImageURL = u
			break
		}
	}
	for _, it := range doc.Channel.Items {
		if it.Enclosure.URL == "" {
			continue // no audio, skip
		}
		id := it.GUID
		if id == "" {
			id = it.Enclosure.URL
		}
		notes := strings.TrimSpace(it.Encoded)
		if notes == "" {
			notes = strings.TrimSpace(it.Description)
		}
		f.Episodes = append(f.Episodes, Episode{
			GUID: id, Title: it.Title, URL: it.Enclosure.URL, Notes: notes,
		})
	}
	return f, nil
}

// FetchFeed downloads and parses a feed URL. ctx controls cancellation/timeout.
func FetchFeed(ctx context.Context, client *http.Client, feedURL string) (Feed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return Feed{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Feed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Feed{}, fmt.Errorf("feed %s: %s", feedURL, resp.Status)
	}
	return ParseFeed(resp.Body)
}

// FetchBytes downloads a small resource (e.g. cover art) into memory,
// failing if it is larger than limit bytes.
func FetchBytes(ctx context.Context, client *http.Client, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", u, limit)
	}
	return data, nil
}

// ReadFeedList parses a feeds file: one URL per line, blank lines and
// "# comments" (whole-line or trailing) are ignored.
func ReadFeedList(r io.Reader) ([]string, error) {
	var urls []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			urls = append(urls, line)
		}
	}
	return urls, sc.Err()
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
