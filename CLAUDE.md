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

- `main.go`: flag parsing, `.downloaded` state file (one GUID per line), `download()` (streams to `.part`, then renames), worker pool with a mutex guarding stdout and state writes
- `feed.go`: `ParseFeed`, `FetchFeed`, `Sanitize`, `FileName`
- `feed_test.go`: tests for parsing, GUID fallback to enclosure URL, and file naming

## Conventions

- Keep it standard library only unless the user asks otherwise.
- Wrap errors with `%w`; print errors to stderr; exit non-zero if any download failed.
- Add or update tests in `feed_test.go` for any change to parsing or naming.
- Downloads and `.downloaded` state live under the output dir (gitignored via `podcasts/`).

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
