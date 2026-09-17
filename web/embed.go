// Package web serves the panel's user interface.
//
// The built frontend is embedded in the binary, so the whole panel is one file
// with nothing to copy alongside it. In development the same handler proxies to
// the Vite dev server instead, which keeps hot reload working without a
// separate command.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// dist holds the built frontend. The placeholder file keeps the embed valid
// before the frontend has been built for the first time.
//
//go:embed all:dist
var dist embed.FS

// Handler returns the handler that serves the panel.
func Handler(devMode bool, devURL string) http.Handler {
	if devMode {
		return devProxy(devURL)
	}
	return embeddedHandler()
}

// embeddedHandler serves the built files, falling back to index.html so that a
// deep link such as /apps/app_123 works on a page reload.
func embeddedHandler() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		slog.Error("the embedded frontend could not be opened", "error", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "the user interface is not available in this build", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if upath == "" {
			upath = "index.html"
		}

		file, err := root.Open(upath)
		if err != nil {
			// Not a file: this is a route inside the single-page app.
			serveIndex(w, r, root)
			return
		}
		info, statErr := file.Stat()
		_ = file.Close()
		if statErr != nil || info.IsDir() {
			serveIndex(w, r, root)
			return
		}

		// Vite fingerprints asset filenames, so they can be cached hard; the
		// HTML entry point must not be, or an upgrade would not take effect.
		if strings.HasPrefix(upath, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, root fs.FS) {
	data, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.Error(w, "the user interface is not available in this build", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(string(data)))
}

// devProxy forwards to the Vite dev server.
func devProxy(target string) http.Handler {
	parsed, err := url.Parse(target)
	if err != nil {
		slog.Error("the dev frontend URL could not be parsed", "url", target, "error", err)
		return embeddedHandler()
	}
	proxy := httputil.NewSingleHostReverseProxy(parsed)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Warn("the frontend dev server is not reachable", "url", target, "error", err)
		http.Error(w,
			"The frontend dev server is not running.\n\nStart it with: npm --prefix web run dev\n",
			http.StatusBadGateway)
	}
	return proxy
}

// HasBuild reports whether a real frontend build is embedded, which the health
// endpoint uses to explain a binary built without the UI.
func HasBuild() bool {
	_, err := fs.Stat(dist, "dist/index.html")
	return err == nil
}

// InlineScriptHashes returns a CSP source for every inline <script> in the
// embedded index.html.
//
// The panel's Content-Security-Policy is `script-src 'self'`, with no
// 'unsafe-inline'. index.html carries one inline script, which reads the stored
// theme and sets the dark class before the first paint so that a dark-mode user
// never sees a white flash — and the policy refused to run it. In production
// only: the policy is not set in dev mode, which is why the flash it exists to
// prevent was visible everywhere except where anybody was looking.
//
// The hash is computed from the file that ships rather than written down beside
// it, because a hash written down beside a script is a hash that goes stale the
// first time somebody edits the script and does not think about the policy.
func InlineScriptHashes() []string {
	data, err := dist.ReadFile("dist/index.html")
	if err != nil {
		return nil
	}
	var out []string
	for _, body := range inlineScripts(string(data)) {
		sum := sha256.Sum256([]byte(body))
		out = append(out, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return out
}

// inlineScripts returns the body of every <script> tag that has no src.
//
// A regular expression rather than a parser: this reads one file this
// repository produces, from a template this repository wrote, and pulling in an
// HTML parser to find two tags in it would be the larger risk.
var scriptTag = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)

func inlineScripts(html string) []string {
	var out []string
	for _, match := range scriptTag.FindAllStringSubmatch(html, -1) {
		if strings.Contains(strings.ToLower(match[1]), "src=") {
			continue
		}
		if strings.TrimSpace(match[2]) == "" {
			continue
		}
		out = append(out, match[2])
	}
	return out
}
