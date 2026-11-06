# podfetch

A small Go CLI that downloads the newest episodes from podcast RSS feeds.

## Features

- One or more feed URLs on the command line, and/or a feeds file (`-f`)
- Downloads the newest N episodes per feed into `<output>/<show title>/`
- Remembers what it already downloaded (`.downloaded` per show), so reruns only fetch new episodes
- Concurrent downloads (worker pool) with a live progress line on a terminal
- Retries with exponential backoff on network errors, stalls, 5xx and 429
- Resumes interrupted downloads (HTTP `Range`) from the `.part` file
- Ctrl-C stops cleanly; partial files are kept and resume on the next run
- Metadata: show notes saved as `<episode>.html`, cover art saved as `cover.jpg/png`,
  and an ID3v2.3 tag (title, show, author, cover) added to MP3s that have none

## Requirements

Go 1.24 or newer. No third-party dependencies.

## Usage

```bash
go run . [flags] [feed-url...]
```

| Flag       | Default    | Meaning                                            |
|------------|------------|----------------------------------------------------|
| `-o`       | `podcasts` | Output directory                                   |
| `-latest`  | `5`        | Newest episodes to consider per feed (0 = all)     |
| `-j`       | `4`        | Concurrent downloads                               |
| `-f`       |            | Feeds file, one URL per line (`#` comments)        |
| `-retries` | `3`        | Retries per download after a failure               |
| `-idle`    | `60s`      | Abort an attempt if no data arrives for this long  |
| `-meta`    | `true`     | Notes, cover art and ID3 tags (`-meta=false` to disable) |

Example `feeds.txt`:

```
# my shows
https://example.com/feed.xml
https://example.org/podcast.rss  # trailing comment
```

```bash
go run . -f feeds.txt -latest 3 -o ~/podcasts
```

Exit code: 0 ok, 1 if any download failed, 2 bad usage, 130 interrupted.
Build a binary with `go build -o podfetch .`.

## Tests

```bash
go test -race ./...
```

## Layout

- `main.go`: flags, orchestration, state file, worker pool, progress, cover art
- `download.go`: `Download` (retry, resume, idle timeout, ID3 prefix on finish)
- `feed.go`: RSS parsing, feed list parsing, file name sanitising
- `id3.go`: ID3v2.3 tag builder
- `*_test.go`: unit tests (downloads use `httptest` servers)
