package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// DownloadOptions tunes Download.
type DownloadOptions struct {
	Retries int           // extra attempts after the first failure
	Idle    time.Duration // abort an attempt if no data arrives for this long (0 = never)
	Backoff time.Duration // first retry delay, doubled each time (default 1s)
	Prefix  []byte        // ID3 tag to prepend once the download is complete (nil = none)
	OnRetry func(attempt int, err error)
}

// httpError is a non-success HTTP status.
type httpError struct {
	Status int
	Text   string
}

func (e *httpError) Error() string { return "http " + e.Text }

// localError is a disk problem; retrying will not help.
type localError struct{ err error }

func (e *localError) Error() string { return e.err.Error() }
func (e *localError) Unwrap() error { return e.err }

var errRestart = errors.New("server refused to resume; restarting")

func retryable(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.Status >= 500 || he.Status == 429 || he.Status == 408
	}
	var le *localError
	return !errors.As(err, &le)
}

// Download fetches url into dest. Data is written to dest+".part"; an existing
// .part file is resumed with an HTTP Range request. Network errors, stalls and
// 5xx/429 responses are retried with exponential backoff. report is called
// with the number of bytes received after each chunk (may be nil).
func Download(ctx context.Context, client *http.Client, url, dest string, o DownloadOptions, report func(n int64)) error {
	part := dest + ".part"
	if o.Backoff <= 0 {
		o.Backoff = time.Second
	}
	var err error
	for attempt := 0; attempt <= o.Retries; attempt++ {
		if attempt > 0 {
			if o.OnRetry != nil {
				o.OnRetry(attempt, err)
			}
			if serr := sleepCtx(ctx, o.Backoff<<(attempt-1)); serr != nil {
				return serr
			}
		}
		err = fetchOnce(ctx, client, url, part, o.Idle, report)
		if err == nil {
			return finalize(part, dest, o.Prefix)
		}
		if ctx.Err() != nil {
			return ctx.Err() // user cancelled: keep .part for next run
		}
		if errors.Is(err, context.Canceled) {
			err = fmt.Errorf("stalled (no data for %s)", o.Idle)
		}
		if !retryable(err) {
			return err
		}
	}
	return fmt.Errorf("giving up after %d attempt(s): %w", o.Retries+1, err)
}

// fetchOnce makes one attempt, resuming from the size of any existing part file.
func fetchOnce(ctx context.Context, client *http.Client, url, part string, idle time.Duration, report func(int64)) error {
	// Per-attempt context: a timer cancels it if the connection goes quiet.
	actx, cancel := context.WithCancel(ctx)
	defer cancel()
	kick := func() {}
	if idle > 0 {
		t := time.AfterFunc(idle, cancel)
		defer t.Stop()
		kick = func() { t.Reset(idle) }
	}

	var offset int64
	if st, err := os.Stat(part); err == nil {
		offset = st.Size()
	}
	req, err := http.NewRequestWithContext(actx, http.MethodGet, url, nil)
	if err != nil {
		return &localError{err}
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusOK: // server ignored Range (or fresh start): start over
		flags |= os.O_TRUNC
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusRequestedRangeNotSatisfiable:
		os.Remove(part) // stale/complete part file: start fresh next attempt
		return errRestart
	default:
		return &httpError{resp.StatusCode, resp.Status}
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return &localError{err}
	}
	defer f.Close()

	buf := make([]byte, 32<<10)
	for {
		kick()
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return &localError{werr}
			}
			if report != nil {
				report(int64(n))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return &localError{err}
	}
	return nil
}

// finalize moves the finished part file to dest, prepending prefix (an ID3
// tag) unless the audio already starts with its own ID3 header.
func finalize(part, dest string, prefix []byte) error {
	if len(prefix) > 0 {
		in, err := os.Open(part)
		if err != nil {
			return &localError{err}
		}
		defer in.Close()
		head, _ := bufio.NewReader(in).Peek(3)
		if !bytes.Equal(head, []byte("ID3")) {
			tmp := dest + ".tag"
			out, err := os.Create(tmp)
			if err != nil {
				return &localError{err}
			}
			_, err = out.Write(prefix)
			if err == nil {
				_, err = in.Seek(0, io.SeekStart) // bufio read ahead; rewind
			}
			if err == nil {
				_, err = io.Copy(out, in)
			}
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				os.Remove(tmp)
				return &localError{err}
			}
			if err := os.Rename(tmp, dest); err != nil {
				return &localError{err}
			}
			in.Close()
			os.Remove(part)
			return nil
		}
	}
	if err := os.Rename(part, dest); err != nil {
		return &localError{err}
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
