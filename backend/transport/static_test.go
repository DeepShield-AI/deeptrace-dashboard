package transport

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStaticDir(t *testing.T) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func serveStatic(dir, url string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	RegisterStatic(mux, dir)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestStaticServesRoot(t *testing.T) {
	dir := newStaticDir(t)
	// "/" maps to index.html internally. (A direct /index.html request gets
	// http.ServeFile's canonical redirect 301 → "./" by design.)
	rec := serveStatic(dir, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "home" {
		t.Fatalf("body = %q, want home", rec.Body.String())
	}
}

func TestStaticServesNonHTMLFile(t *testing.T) {
	dir := newStaticDir(t)
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := serveStatic(dir, "/app.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "console.log(1)" {
		t.Fatalf("body = %q, want file content", rec.Body.String())
	}
}

func TestStaticRejectsEncodedTraversal(t *testing.T) {
	dir := newStaticDir(t)
	// These reached the handler with an un-cleaned path before the fix
	// (encoded ".." bypasses ServeMux's cleanPath redirect).
	for _, u := range []string{
		"/%2e%2e/%2e%2e/etc/passwd",
		"/..%2f..%2fetc/passwd",
		"/%2e%2e%2f%2e%2e%2fetc/passwd",
	} {
		rec := serveStatic(dir, u)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404 (no file leak)", u, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "root:") || strings.Contains(rec.Body.String(), "bin:") {
			t.Fatalf("%s: leaked file content: %q", u, rec.Body.String())
		}
	}
}

func TestStaticSpaFallbackStillServesIndex(t *testing.T) {
	dir := newStaticDir(t)
	rec := serveStatic(dir, "/some/spa/route")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "home" {
		t.Fatalf("body = %q, want SPA index", rec.Body.String())
	}
}
