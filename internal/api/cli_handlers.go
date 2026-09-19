package api

import (
	"net/http"
	"os"
	"runtime"

	"skifity/internal/errdoc"
	"skifity/internal/version"
)

// The panel hands out the command line tool it is itself running.
//
// One binary is the panel, the CLI and the MCP server, so the file serving this
// request *is* the file somebody wants on their PATH. That turns out to matter
// twice over.
//
// The installer used to fetch the CLI from a releases page, and there is no
// release: no releases page existed, so every install ended
// with "could not download the command line tool" and a link to nothing. It now
// asks the panel it has just started, which always answers and always answers
// with the matching version — a CLI one release behind its panel is a class of
// bug report nobody enjoys.
//
// Open, like the frontend beside it. There is nothing in the binary that is not
// in the source, and requiring a token to download the thing you obtain tokens
// with is a circle.

// handleDownloadCLI streams the running binary.
func (s *Server) handleDownloadCLI(w http.ResponseWriter, r *http.Request) {
	path, err := os.Executable()
	if err != nil {
		writeError(w, r, cliUnavailable(err))
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, r, cliUnavailable(err))
		return
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		writeError(w, r, cliUnavailable(err))
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+version.Binary+`"`)
	// Not cached: the panel upgrades in place, and a stale copy of a CLI is
	// exactly the version mismatch this endpoint exists to prevent.
	w.Header().Set("Cache-Control", "no-store")
	// ServeContent rather than io.Copy: it answers a Range request, which is
	// what makes an interrupted download resumable over a bad connection.
	http.ServeContent(w, r, version.Binary, info.ModTime(), file)
}

// cliPlatform is what the binary this panel is running was built for. It is in
// /api/meta so that a page or a script can say "this is the linux/amd64 build"
// rather than handing somebody a file that will not run.
func cliPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }

func cliUnavailable(err error) error {
	return errdoc.New("cli.unavailable", "This panel cannot hand out its own binary").
		WithCause("The running executable could not be read: %s", err.Error()).
		WithImpact("The command line tool was not downloaded. The panel itself is unaffected.").
		WithFix("Build it from the repository with `make build`, or copy the binary from another machine. The panel and the CLI are the same file.").
		WithStatus(http.StatusInternalServerError)
}
