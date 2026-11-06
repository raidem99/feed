package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// loadState reads the set of already-downloaded GUIDs (one per line).
func loadState(p string) map[string]bool {
	seen := map[string]bool{}
	f, err := os.Open(p)
	if err != nil {
		return seen // first run: no file yet
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		seen[sc.Text()] = true
	}
	return seen
}

func appendState(p, guid string) error {
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, guid)
	return err
}

// console serialises output from many goroutines and, on a terminal, keeps a
// live progress line at the bottom that log lines temporarily overwrite.
type console struct {
	mu  sync.Mutex
	tty bool
}

func (c *console) logf(w io.Writer, format string, a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tty {
		fmt.Fprint(os.Stdout, "\r\033[K") // clear progress line
	}
	fmt.Fprintf(w, format, a...)
}

func (c *console) status(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(os.Stdout, "\r\033[K%s", s)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

// showProgress prints "[done/total] bytes, speed" once a second until stop closes.
func showProgress(c *console, total int, bytes *atomic.Int64, done *atomic.Int32, stop <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var last int64
	for {
		select {
		case <-stop:
			c.logf(os.Stdout, "")
			return
		case <-t.C:
			cur := bytes.Load()
			c.status(fmt.Sprintf("[%d/%d] %s received, %s/s", done.Load(), total, humanBytes(cur), humanBytes(cur-last)))
			last = cur
		}
	}
}

type job struct {
	ep     Episode
	show   string
	author string
	cover  []byte // may be nil
	dest   string
	state  string
}

func main() { os.Exit(run()) }

func run() int {
	out := flag.String("o", "podcasts", "output directory")
	latest := flag.Int("latest", 5, "number of newest episodes to consider per feed (0 = all)")
	workers := flag.Int("j", 4, "concurrent downloads")
	feedFile := flag.String("f", "", "file with one feed URL per line (# comments allowed)")
	retries := flag.Int("retries", 3, "retries per download after a failure")
	idle := flag.Duration("idle", 60*time.Second, "abort an attempt if no data arrives for this long")
	meta := flag.Bool("meta", true, "save show notes (.html), cover art, and write ID3 tags to MP3s")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: podfetch [flags] [feed-url...]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	feeds := flag.Args()
	if *feedFile != "" {
		f, err := os.Open(*feedFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		list, err := ReadFeedList(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		feeds = append(feeds, list...)
	}
	if len(feeds) == 0 {
		flag.Usage()
		return 2
	}
	if *workers < 1 {
		*workers = 1
	}

	// Ctrl-C / SIGTERM cancel ctx; everything below watches it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, _ := os.Stdout.Stat()
	con := &console{tty: st != nil && st.Mode()&os.ModeCharDevice != 0}

	client := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 30 * time.Second,
	}}

	var jobs []job
	for _, feedURL := range feeds {
		if ctx.Err() != nil {
			break
		}
		fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		feed, err := FetchFeed(fctx, client, feedURL)
		cancel()
		if err != nil {
			con.logf(os.Stderr, "error: %v\n", err)
			continue
		}
		dir := filepath.Join(*out, Sanitize(feed.Title))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			con.logf(os.Stderr, "error: %v\n", err)
			continue
		}
		statePath := filepath.Join(dir, ".downloaded")
		seen := loadState(statePath)
		eps := feed.Episodes
		if *latest > 0 && len(eps) > *latest {
			eps = eps[:*latest]
		}
		var fresh []job
		for _, e := range eps {
			if !seen[e.GUID] {
				fresh = append(fresh, job{ep: e, show: feed.Title, author: feed.Author,
					dest: filepath.Join(dir, FileName(e)), state: statePath})
			}
		}
		con.logf(os.Stdout, "%s: %d new episode(s)\n", feed.Title, len(fresh))
		if len(fresh) > 0 && *meta && feed.ImageURL != "" {
			cover := fetchCover(ctx, client, feed.ImageURL, dir, con)
			for i := range fresh {
				fresh[i].cover = cover
			}
		}
		jobs = append(jobs, fresh...)
	}

	var (
		totalBytes atomic.Int64
		doneCount  atomic.Int32
		failed     atomic.Int32
		stateMu    sync.Mutex
		wg         sync.WaitGroup
	)
	progressStop := make(chan struct{})
	progressDone := make(chan struct{})
	if con.tty && len(jobs) > 0 {
		go func() {
			showProgress(con, len(jobs), &totalBytes, &doneCount, progressStop)
			close(progressDone)
		}()
	} else {
		close(progressDone)
	}

	// Worker pool: goroutines read jobs from a channel.
	ch := make(chan job)
	start := time.Now()
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				if ctx.Err() != nil {
					continue // cancelled: drain without starting new work
				}
				opts := DownloadOptions{
					Retries: *retries,
					Idle:    *idle,
					OnRetry: func(n int, err error) {
						con.logf(os.Stderr, "retry %d/%d %s: %v\n", n, *retries, j.ep.Title, err)
					},
				}
				if *meta && strings.EqualFold(filepath.Ext(j.dest), ".mp3") {
					opts.Prefix = BuildID3(j.ep.Title, j.show, j.author, j.cover)
				}
				err := Download(ctx, client, j.ep.URL, j.dest, opts, func(n int64) { totalBytes.Add(n) })
				if err != nil {
					if ctx.Err() == nil {
						failed.Add(1)
						con.logf(os.Stderr, "FAIL %s: %v\n", j.ep.Title, err)
					}
					continue
				}
				if *meta && j.ep.Notes != "" {
					notes := strings.TrimSuffix(j.dest, filepath.Ext(j.dest)) + ".html"
					if err := os.WriteFile(notes, []byte(j.ep.Notes), 0o644); err != nil {
						con.logf(os.Stderr, "notes: %v\n", err)
					}
				}
				stateMu.Lock()
				serr := appendState(j.state, j.ep.GUID)
				stateMu.Unlock()
				if serr != nil {
					con.logf(os.Stderr, "state: %v\n", serr)
				}
				doneCount.Add(1)
				size := int64(0)
				if fi, err := os.Stat(j.dest); err == nil {
					size = fi.Size()
				}
				con.logf(os.Stdout, "ok   %s (%s)\n", j.dest, humanBytes(size))
			}
		}()
	}
loop:
	for _, j := range jobs {
		select {
		case ch <- j:
		case <-ctx.Done():
			break loop
		}
	}
	close(ch)
	wg.Wait()
	close(progressStop)
	<-progressDone

	if len(jobs) > 0 {
		con.logf(os.Stdout, "%d downloaded, %d failed, %s in %s\n",
			doneCount.Load(), failed.Load(), humanBytes(totalBytes.Load()), time.Since(start).Round(time.Second))
	}
	if ctx.Err() != nil {
		con.logf(os.Stderr, "interrupted: partial files kept as .part and will resume on the next run\n")
		return 130
	}
	if failed.Load() > 0 {
		return 1
	}
	return 0
}

// fetchCover downloads cover art (max 10 MiB), saves it as cover.jpg/png in
// dir if not already there, and returns the bytes for embedding. Problems are
// warnings, not errors: it returns nil.
func fetchCover(ctx context.Context, client *http.Client, u, dir string, con *console) []byte {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := FetchBytes(cctx, client, u, 10<<20)
	if err != nil {
		con.logf(os.Stderr, "cover art: %v\n", err)
		return nil
	}
	var ext string
	switch http.DetectContentType(data) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	default:
		con.logf(os.Stderr, "cover art: unsupported image type from %s\n", u)
		return nil
	}
	p := filepath.Join(dir, "cover"+ext)
	if _, err := os.Stat(p); err != nil {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			con.logf(os.Stderr, "cover art: %v\n", err)
		}
	}
	return data
}
