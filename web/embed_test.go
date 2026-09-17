package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A missing file must be a 404, and a route must be the app.
//
// Falling back to index.html for everything is the usual shortcut and it hurts
// where it is hardest to diagnose: a browser holding a cached page asks for a
// chunk a new deploy has renamed, gets HTML with a 200, and the JavaScript
// parser fails on `<!doctype html>`. What the person sees is a white page and a
// syntax error in a file they did not write.
func TestAMissingFileIsNotTheSinglePageApp(t *testing.T) {
	handler := Handler(false, "")

	for _, path := range []string{
		"/assets/index-DEADBEEF.js",
		"/assets/style-0000.css",
		"/nothing.js",
		"/deep/path/missing.css",
		"/missing.svg",
		"/missing.json",
	} {
		response := get(t, handler, path)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s answered %d with %q, want 404 — a browser asking for a "+
				"renamed chunk must be told it is gone, not handed HTML",
				path, response.Code, response.Header().Get("Content-Type"))
		}
	}

	// A route inside the app has no extension, and reloading on one has to
	// work: that is what the fallback is for.
	for _, path := range []string{
		"/",
		"/apps/app_06gax1hb07nr6yx4edmn",
		"/settings",
		"/projects/proj_1/environments",
	} {
		response := get(t, handler, path)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s answered %d, want the app", path, response.Code)
		}
		if !strings.Contains(response.Header().Get("Content-Type"), "text/html") {
			t.Errorf("GET %s answered %q, want HTML", path, response.Header().Get("Content-Type"))
		}
	}
}

// TestAFingerprintedAssetIsCachedAndTheEntryPointIsNot: an asset's name
// contains its own hash, so it can be cached forever; index.html must not be,
// or an upgrade would not take effect until every browser gave up its copy.
func TestAFingerprintedAssetIsCachedAndTheEntryPointIsNot(t *testing.T) {
	handler := Handler(false, "")

	if cache := get(t, handler, "/").Header().Get("Cache-Control"); !strings.Contains(cache, "no-cache") {
		t.Errorf("index.html is served with %q; an upgrade would not take effect", cache)
	}

	// Whichever chunk this build produced.
	entries, err := dist.ReadDir("dist/assets")
	if err != nil || len(entries) == 0 {
		t.Skip("this build has no frontend embedded")
	}
	name := entries[0].Name()

	cache := get(t, handler, "/assets/"+name).Header().Get("Cache-Control")
	if !strings.Contains(cache, "immutable") {
		t.Errorf("/assets/%s is served with %q, want it cached hard: the name contains its hash",
			name, cache)
	}
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
