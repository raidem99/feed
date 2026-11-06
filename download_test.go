package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

var audio = bytes.Repeat([]byte("0123456789"), 1000) // 10 kB, no ID3 header

func fast() DownloadOptions { return DownloadOptions{Retries: 3, Backoff: time.Millisecond} }

// serve returns a server that supports Range requests and counts hits.
func serve(t *testing.T, hits *atomic.Int32, wrap func(http.HandlerFunc) http.HandlerFunc) *httptest.Server {
	t.Helper()
	h := func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x.mp3", time.Time{}, bytes.NewReader(audio))
	}
	if wrap != nil {
		h = wrap(h)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadOK(t *testing.T) {
	var hits atomic.Int32
	srv := serve(t, &hits, nil)
	dest := filepath.Join(t.TempDir(), "a.mp3")
	var got atomic.Int64
	err := Download(context.Background(), srv.Client(), srv.URL, dest, fast(), func(n int64) { got.Add(n) })
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	if !bytes.Equal(data, audio) {
		t.Error("content mismatch")
	}
	if got.Load() != int64(len(audio)) {
		t.Errorf("reported %d bytes", got.Load())
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Error(".part left behind")
	}
}

func TestDownloadRetriesServerError(t *testing.T) {
	var hits atomic.Int32
	srv := serve(t, &hits, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if hits.Load() == 1 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			next(w, r)
		}
	})
	dest := filepath.Join(t.TempDir(), "a.mp3")
	if err := Download(context.Background(), srv.Client(), srv.URL, dest, fast(), nil); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Errorf("hits = %d, want 2", hits.Load())
	}
}

func TestDownloadNoRetryOn404(t *testing.T) {
	var hits atomic.Int32
	srv := serve(t, &hits, func(http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }
	})
	dest := filepath.Join(t.TempDir(), "a.mp3")
	if err := Download(context.Background(), srv.Client(), srv.URL, dest, fast(), nil); err == nil {
		t.Fatal("expected error")
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1 (404 is not retryable)", hits.Load())
	}
}

func TestDownloadResumes(t *testing.T) {
	var hits atomic.Int32
	var rangeHdr atomic.Value
	srv := serve(t, &hits, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			rangeHdr.Store(r.Header.Get("Range"))
			next(w, r)
		}
	})
	dest := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(dest+".part", audio[:4000], 0o644)

	var got atomic.Int64
	if err := Download(context.Background(), srv.Client(), srv.URL, dest, fast(), func(n int64) { got.Add(n) }); err != nil {
		t.Fatal(err)
	}
	if rangeHdr.Load() != "bytes=4000-" {
		t.Errorf("Range = %v", rangeHdr.Load())
	}
	data, _ := os.ReadFile(dest)
	if !bytes.Equal(data, audio) {
		t.Error("resumed content mismatch")
	}
	if got.Load() != 6000 {
		t.Errorf("only the remainder should be fetched; got %d", got.Load())
	}
}

func TestDownloadRestartsWhenRangeIgnored(t *testing.T) {
	var hits atomic.Int32
	srv := serve(t, &hits, func(http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { w.Write(audio) } // always 200
	})
	dest := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(dest+".part", []byte("garbage"), 0o644)
	if err := Download(context.Background(), srv.Client(), srv.URL, dest, fast(), nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	if !bytes.Equal(data, audio) {
		t.Error("content should be the full file, not garbage+audio")
	}
}

func TestDownloadIdleTimeout(t *testing.T) {
	var hits atomic.Int32
	srv := serve(t, &hits, func(http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			select { // stall until the client gives up
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}
	})
	o := fast()
	o.Retries = 1
	o.Idle = 50 * time.Millisecond
	start := time.Now()
	err := Download(context.Background(), srv.Client(), srv.URL, filepath.Join(t.TempDir(), "a.mp3"), o, nil)
	if err == nil {
		t.Fatal("expected stall error")
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %s; idle timeout not applied", time.Since(start))
	}
	if hits.Load() != 2 {
		t.Errorf("hits = %d, want 2 (one retry)", hits.Load())
	}
}

func TestDownloadCancelKeepsPart(t *testing.T) {
	var hits atomic.Int32
	srv := serve(t, &hits, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dest := filepath.Join(t.TempDir(), "a.mp3")
	if err := Download(ctx, srv.Client(), srv.URL, dest, fast(), nil); err == nil {
		t.Fatal("expected cancellation error")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("final file must not exist after cancel")
	}
}

func TestDownloadPrefix(t *testing.T) {
	var hits atomic.Int32
	srv := serve(t, &hits, nil)
	o := fast()
	o.Prefix = BuildID3("T", "A", "", nil)

	dest := filepath.Join(t.TempDir(), "a.mp3")
	if err := Download(context.Background(), srv.Client(), srv.URL, dest, o, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	if !bytes.Equal(data, append(append([]byte{}, o.Prefix...), audio...)) {
		t.Error("expected tag followed by untouched audio")
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Error(".part left behind")
	}
}

func TestDownloadKeepsExistingID3(t *testing.T) {
	tagged := append([]byte("ID3\x03\x00\x00\x00\x00\x00\x00"), audio...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tagged)
	}))
	defer srv.Close()
	o := fast()
	o.Prefix = BuildID3("T", "A", "", nil)
	dest := filepath.Join(t.TempDir(), "a.mp3")
	if err := Download(context.Background(), srv.Client(), srv.URL, dest, o, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	if !bytes.Equal(data, tagged) {
		t.Error("file with its own ID3 tag must be left untouched")
	}
}
