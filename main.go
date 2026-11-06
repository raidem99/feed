package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
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

// download streams url to dest via a temp file, so a crash never leaves a
// half-written file under the final name.
func download(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func main() {
	out := flag.String("o", "podcasts", "output directory")
	latest := flag.Int("latest", 5, "number of newest episodes to consider per feed (0 = all)")
	workers := flag.Int("j", 4, "concurrent downloads")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: podfetch [flags] <feed-url>...\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	// No overall timeout: big files take a while. Header timeout guards stalls.
	client := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 30 * time.Second,
	}}

	type job struct {
		ep    Episode
		dest  string
		state string
	}
	var jobs []job

	for _, feedURL := range flag.Args() {
		title, eps, err := FetchFeed(client, feedURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			continue
		}
		dir := filepath.Join(*out, Sanitize(title))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			continue
		}
		statePath := filepath.Join(dir, ".downloaded")
		seen := loadState(statePath)
		if *latest > 0 && len(eps) > *latest {
			eps = eps[:*latest]
		}
		for _, e := range eps {
			if seen[e.GUID] {
				continue
			}
			jobs = append(jobs, job{e, filepath.Join(dir, FileName(e)), statePath})
		}
		fmt.Printf("%s: %d new episode(s)\n", title, len(eps))
	}

	// Worker pool: goroutines read jobs from a channel. A mutex serialises
	// writes to the state files and stdout.
	ch := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				err := download(client, j.ep.URL, j.dest)
				mu.Lock()
				if err != nil {
					failed++
					fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", j.ep.Title, err)
				} else {
					fmt.Println("ok  ", j.dest)
					if err := appendState(j.state, j.ep.GUID); err != nil {
						fmt.Fprintln(os.Stderr, "state:", err)
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()

	if failed > 0 {
		os.Exit(1)
	}
}
