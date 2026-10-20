package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The narration mp3s live on disk (they are far too large to embed); the
// /media/ branch must stream them, set a playable content type, and refuse to
// walk above the media root — otherwise any request could download the SQLite
// database sitting in the same directory.
func TestAssetHandlerServesMediaAndBlocksTraversal(t *testing.T) {
	mediaRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mediaRoot, "tts"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Decoy outside the media root: must never be reachable through /media/.
	secret := filepath.Join(filepath.Dir(mediaRoot), "secret.txt")
	if err := os.WriteFile(secret, []byte("db-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	if err := os.WriteFile(filepath.Join(mediaRoot, "tts", "7.mp3"), []byte("ID3audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := assetHandler(mediaRoot)

	t.Run("serves mp3", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/tts/7.mp3", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "audio") && !strings.Contains(got, "mpeg") {
			t.Fatalf("content type = %q, want audio/mpeg (or platform default)", got)
		}
		if rec.Body.String() != "ID3audio" {
			t.Fatalf("body = %q", rec.Body.String())
		}
	})

	t.Run("traversal is rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/../secret.txt", nil))
		if rec.Body.String() == "db-bytes" {
			t.Fatal("escaped the media root")
		}
	})

	t.Run("frontend fallback still works", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/spa/route", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("SPA fallback status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Fatalf("content type = %q, want html", ct)
		}
	})
}
