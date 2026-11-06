# CLAUDE.md

Guidance for Claude when working in this repository.

## Project

`podfetch`: a Go CLI that downloads the newest episodes from podcast RSS feeds.
Standard library only, single `main` package, module name `podfetch`.

## Commands

- Test: `go test ./...`
- Lint: `gofmt -l .` and `go vet ./...` (both must be clean)
- Run: `go run . -latest 3 -o podcasts <feed-url>`

## Code map

- `main.go`: flags, signal handling (`signal.NotifyContext`), `.downloaded` state file (one GUID per line), worker pool, `console` (mutex-guarded output + TTY progress line), `fetchCover`
- `download.go`: `Download` = retry loop + `fetchOnce` (Range resume, idle timer) + `finalize` (rename, optional ID3 prefix). Data goes to `<dest>.part`; the ID3 tag is added only at the end so resume offsets stay equal to the server offsets
- `feed.go`: `ParseFeed` (returns `Feed`), `FetchFeed`, `FetchBytes`, `ReadFeedList`, `Sanitize`, `FileName`
- `id3.go`: `BuildID3` (v2.3, UTF-16 text frames, APIC cover)
- tests: `feed_test.go`, `id3_test.go`, `download_test.go` (httptest servers)

## Conventions

- Keep it standard library only unless the user asks otherwise.
- Wrap errors with `%w`; print errors to stderr; exit non-zero if any download failed.
- Add or update tests next to the code you change (parsing/naming in `feed_test.go`, downloads in `download_test.go`).
- Check with `go test -race ./...`, `gofmt -l .`, `go vet ./...`.
- Local files are never retried on error (`localError`); HTTP 4xx other than 408/429 are not retried.
- Downloads and `.downloaded` state live under the output dir (gitignored via `podcasts/`).

## Known gaps

- Episodes with identical titles share a file name and overwrite each other (idea: date/GUID suffix).
- No `pubDate` parsing, so ordering trusts the feed.

## About the user

Experienced Python developer, new to Go. Explain Go-specific idioms briefly
(goroutines, channels, struct tags, error handling) and keep code simple and
commented where it helps. Keep replies short by default, but give full detail
when debugging or reporting errors. Ask up to 5 clarifying questions when a
request is vague.

## Git

- Default branch: `main`.
- Do not push or open PRs unless the user asks.
- Commit messages: short imperative subject, brief body.
