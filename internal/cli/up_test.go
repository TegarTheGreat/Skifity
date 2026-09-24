package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// write puts files into a folder, creating the directories they need.
func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// entries reads back what an archive holds.
func entries(t *testing.T, archive string) map[string]*tar.Header {
	t.Helper()
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	out := map[string]*tar.Header{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[header.Name] = header
	}
}

func packNames(t *testing.T, root string) []string {
	t.Helper()
	folder, err := pack(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(folder.Archive) })
	var names []string
	for name := range entries(t, folder.Archive) {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestUpSendsTheAppAndNotWhatIsAroundIt(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"package.json":                "{}",
		"src/index.js":                "1",
		".env":                        "STRIPE_KEY=sk_live_x",
		".env.local":                  "X=1",
		"api/.env.production":         "X=1",
		".env.example":                "STRIPE_KEY=",
		"node_modules/react/index.js": "x",
		".git/config":                 "x",
		".next/cache/a":               "x",
		"skifity.toml":                `app = "app_1"`,
		".gitignore":                  "dist\n*.sqlite\n/secret.txt\n!keep.sqlite\nlogs/\n",
		"dist/bundle.js":              "x",
		"data/app.sqlite":             "x",
		"keep.sqlite":                 "x",
		"secret.txt":                  "x",
		"sub/secret.txt":              "kept: the rule is anchored to the root",
		"logs/today":                  "x",
		"sub/.gitignore":              "local.txt\n",
		"sub/local.txt":               "x",
		"local.txt":                   "kept: the rule is only for sub/",
		".skifityignore":              "fixtures/\n",
		"fixtures/big.json":           "x",
	})
	got := packNames(t, root)
	want := []string{
		".env.example", ".gitignore", ".skifityignore", "keep.sqlite", "local.txt",
		"package.json", "src/index.js", "sub/.gitignore", "sub/secret.txt",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sent %v\nwant %v", got, want)
	}
}

// A .env is never sent, whatever an ignore file says. The panel refuses one
// too, but this is the line that keeps it off the network at all.
func TestAnIgnoreFileCannotBringBackTheSecrets(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"index.html":              "x",
		".env":                    "KEY=1",
		".gitignore":              "!.env\n!node_modules/\n",
		"node_modules/x/index.js": "x",
	})
	if got := packNames(t, root); !slices.Equal(got, []string{".gitignore", "index.html"}) {
		t.Fatalf("sent %v", got)
	}
}

// The same folder packs to the same bytes, so sending it again is the same
// upload and the panel reuses the image rather than building for nothing.
func TestTheSameFolderIsTheSameUpload(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"b.txt": "2", "a/c.txt": "3", "index.html": "1"})
	hash := func() [32]byte {
		folder, err := pack(root)
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(folder.Archive)
		data, err := os.ReadFile(folder.Archive)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(data)
	}
	first := hash()
	// Touched but not changed: a save in an editor, a checkout.
	later := filepath.Join(root, "b.txt")
	if err := os.Chtimes(later, timeNowPlus(t), timeNowPlus(t)); err != nil {
		t.Fatal(err)
	}
	if hash() != first {
		t.Fatal("touching a file changed the upload, so the same code would build again")
	}
	write(t, root, map[string]string{"b.txt": "changed"})
	if hash() == first {
		t.Fatal("changed code made the same upload, so it would never be built")
	}
}

func TestAScriptStaysRunnableAndALinkStaysBehind(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"gradlew": "#!/bin/sh", "index.html": "x"})
	if err := os.Chmod(filepath.Join(root, "gradlew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "passwd")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	folder, err := pack(root)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(folder.Archive)
	got := entries(t, folder.Archive)
	if got["gradlew"] == nil || got["gradlew"].Mode&0o111 == 0 {
		t.Fatal("an executable lost its executable bit")
	}
	if got["passwd"] != nil || !slices.Equal(folder.Skipped, []string{"passwd"}) {
		t.Fatalf("a link was sent, or not reported as left behind: %v", folder.Skipped)
	}
}

// Running `up` from inside an app sends the whole app. Sending the subfolder
// the terminal was in would replace the app with a corner of itself.
func TestUpFromASubfolderSendsTheWholeApp(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"skifity.toml": `app = "app_1"`, "src/main.go": "package main"})
	found, file, err := upRoot([]string{filepath.Join(root, "src")})
	if err != nil {
		t.Fatal(err)
	}
	if found != root || file.App != "app_1" {
		t.Fatalf("from src/ it would send %s for app %q", found, file.App)
	}
}

func TestADotEnvIsReadLikeThePanelReadsOne(t *testing.T) {
	got := parseDotEnv("# comment\nexport A=1\nB=\"two words\"\nC='x=y'\n bad line\n9X=no\nD=\n")
	want := map[string]string{"A": "1", "B": "two words", "C": "x=y", "D": ""}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %q, want %q", key, got[key], value)
		}
	}
}

func TestAFolderNameBecomesAnAppName(t *testing.T) {
	for folder, want := range map[string]string{
		"My Cool App": "my-cool-app", "todo_list": "todo-list", "  ": "app", "网站": "app",
	} {
		if got := friendlyName(folder); got != want {
			t.Errorf("%q became %q, want %q", folder, got, want)
		}
	}
}

func timeNowPlus(t *testing.T) (when time.Time) {
	t.Helper()
	return time.Now().Add(time.Hour)
}
