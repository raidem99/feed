# podfetch

A small Go CLI that downloads the newest episodes from podcast RSS feeds.

## Features

- Takes one or more RSS feed URLs
- Downloads the newest N episodes per feed into `<output>/<show title>/`
- Remembers what it already downloaded (`.downloaded` file per show), so reruns only fetch new episodes
- Concurrent downloads via a worker pool
- Writes to a `.part` file and renames on success, so no half-finished files

## Requirements

Go 1.24 or newer. No third-party dependencies.

## Usage

```bash
go run . [flags] <feed-url>...
```

| Flag      | Default    | Meaning                                         |
|-----------|------------|-------------------------------------------------|
| `-o`      | `podcasts` | Output directory                                |
| `-latest` | `5`        | Newest episodes to consider per feed (0 = all)  |
| `-j`      | `4`        | Concurrent downloads                            |

Example:

```bash
go run . -latest 3 -o ~/podcasts https://example.com/feed.xml
```

Build a binary with `go build -o podfetch .`.

## Tests

```bash
go test ./...
```

## Layout

- `main.go`: flags, state file, download worker pool
- `feed.go`: RSS parsing, file name sanitising
- `feed_test.go`: unit tests
