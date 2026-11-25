package casv11

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedCasBlob(t *testing.T, casRoot string, content []byte) (string, string) {
	t.Helper()
	sum := sha512.Sum512(content)
	digest := hex.EncodeToString(sum[:])
	blobDir := filepath.Join(casRoot, "files", digest[:2])
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	blobPath := filepath.Join(blobDir, digest)
	if err := os.WriteFile(blobPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return digest, blobPath
}

func TestCasV11PagesHandler_200OK(t *testing.T) {
	t.Parallel()

	casRoot := t.TempDir()
	htmlBody := []byte("<!doctype html><h1>Awesome-OS Pages v11</h1>")
	digest, _ := seedCasBlob(t, casRoot, htmlBody)

	rawManifest, err := json.Marshal(SiteManifest{
		Routes: map[string]string{
			"/index.html": digest,
		},
	})
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}

	manifest, err := ParseSiteManifest(rawManifest)
	if err != nil {
		t.Fatalf("ParseSiteManifest: %v", err)
	}

	handler, err := NewCasV11PagesHandler(casRoot, manifest)
	if err != nil {
		t.Fatalf("NewCasV11PagesHandler: %v", err)
	}

	for _, target := range []string{"/index.html", "/"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: got status %d, want 200", target, res.StatusCode)
		}
		if got := res.Header.Get("ETag"); got != `"`+digest+`"` {
			t.Fatalf("GET %s: ETag got %q, want %q", target, got, `"`+digest+`"`)
		}
		if got := res.Header.Get("X-B8G-CAS"); got != "v11" {
			t.Fatalf("GET %s: X-B8G-CAS got %q, want %q", target, got, "v11")
		}
		if got := res.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
			t.Fatalf("GET %s: Content-Type got %q, want text/html", target, got)
		}
		if rec.Body.String() != string(htmlBody) {
			t.Fatalf("GET %s: body mismatch: got %q, want %q", target, rec.Body.String(), string(htmlBody))
		}
	}
}

func TestCasV11PagesHandler_304NotModified(t *testing.T) {
	t.Parallel()

	casRoot := t.TempDir()
	body := []byte("body { color: #00ff88; }")
	digest, _ := seedCasBlob(t, casRoot, body)

	handler, err := NewCasV11PagesHandler(casRoot, &SiteManifest{
		Routes: map[string]string{
			"/style.css": digest,
		},
	})
	if err != nil {
		t.Fatalf("NewCasV11PagesHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	req.Header.Set("If-None-Match", `"`+digest+`"`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body on 304, got %d bytes", rec.Body.Len())
	}
	if got := rec.Header().Get("ETag"); got != `"`+digest+`"` {
		t.Fatalf("ETag got %q, want %q", got, `"`+digest+`"`)
	}
	if got := rec.Header().Get("X-B8G-CAS"); got != "v11" {
		t.Fatalf("X-B8G-CAS got %q, want v11", got)
	}
}

func TestCasV11PagesHandler_404NotFound(t *testing.T) {
	t.Parallel()

	casRoot := t.TempDir()
	handler, err := NewCasV11PagesHandler(casRoot, &SiteManifest{
		Routes: map[string]string{},
	})
	if err != nil {
		t.Fatalf("NewCasV11PagesHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-B8G-CAS"); got != "v11" {
		t.Fatalf("X-B8G-CAS got %q, want v11", got)
	}
}

func TestCasV11PagesHandler_PathTraversalRejection(t *testing.T) {
	t.Parallel()

	casRoot := t.TempDir()
	digest, _ := seedCasBlob(t, casRoot, []byte("ok"))

	handler, err := NewCasV11PagesHandler(casRoot, &SiteManifest{
		Routes: map[string]string{
			"/index.html": digest,
		},
	})
	if err != nil {
		t.Fatalf("NewCasV11PagesHandler: %v", err)
	}

	traversalPaths := []string{
		"/../etc/passwd",
		"/nested/../../secret.txt",
		"/%2e%2e/secret.txt",
		"/..\\secret.txt",
	}

	for _, p := range traversalPaths {
		req := httptest.NewRequest(http.MethodGet, "http://pages.local/", nil)
		req.URL.Path = p
		req.RequestURI = p
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %q: expected 400 Bad Request, got %d", p, rec.Code)
		}
		if got := rec.Header().Get("X-B8G-CAS"); got != "v11" {
			t.Fatalf("path %q: X-B8G-CAS got %q, want v11", p, got)
		}
	}
}

func TestCasV11PagesHandler_IntegrityMismatch(t *testing.T) {
	t.Parallel()

	casRoot := t.TempDir()
	digest, blobPath := seedCasBlob(t, casRoot, []byte("original"))
	if err := os.WriteFile(blobPath, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("tamper WriteFile: %v", err)
	}

	handler, err := NewCasV11PagesHandler(casRoot, &SiteManifest{
		Routes: map[string]string{
			"/index.html": digest,
		},
	})
	if err != nil {
		t.Fatalf("NewCasV11PagesHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on tampered blob, got %d", rec.Code)
	}
}
