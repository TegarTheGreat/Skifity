package backup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"skifity/internal/dbsvc"
)

// The dump script is a shell program, so it is tested by running it.
//
// Reading it cannot tell an empty backup from a full one. The check that a
// dump is worth uploading used to happen after gzip, and compressing an empty
// file gives about twenty bytes — which every "is this file non-empty" test
// accepts. A dump that produced nothing while reporting success was uploaded,
// recorded, and found to be worthless on the day somebody needed it.
//
// runDumpScript runs the script with a stub dump tool that writes whatever the
// test tells it to, and reports whether the script accepted the result.
func runDumpScript(t *testing.T, engine, tool, output string) (ok bool, combined string) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no POSIX shell here")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stub dump tool. It writes what the test asked for and succeeds, which
	// is exactly the case the size check exists for.
	stub := "#!/bin/sh\nprintf '%s' " + shellQuote(output) + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, tool), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	script := backupScript(JobSpec{Engine: engine})
	// The script writes beside dumpFile, so give it somewhere writable.
	if err := os.MkdirAll(filepath.Dir(dumpFile), 0o755); err != nil {
		t.Skipf("cannot create %s here: %v", filepath.Dir(dumpFile), err)
	}
	t.Cleanup(func() {
		_ = os.Remove(dumpFile)
		_ = os.Remove(dumpFile + ".raw")
	})

	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"DB_NAME=shop", "DB_HOST=db.test", "DB_PORT=5432",
		"DB_USER=shop", "DB_PASSWORD=not-a-real-password")
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// An empty dump has to stop the backup, not become one.
func TestADumpThatProducedNothingIsRefused(t *testing.T) {
	ok, out := runDumpScript(t, dbsvc.EnginePostgres, "pg_dump", "")
	if ok {
		t.Fatalf("an empty dump was accepted and would have been uploaded as a backup:\n%s", out)
	}
	if !strings.Contains(out, "came back empty") {
		t.Errorf("the reason was not said in words somebody can act on:\n%s", out)
	}
}

// A real dump still goes through, compressed.
func TestADumpWithContentIsKept(t *testing.T) {
	ok, out := runDumpScript(t, dbsvc.EnginePostgres, "pg_dump",
		"--\n-- PostgreSQL database dump\n--\nCREATE TABLE orders (id int);\n")
	if !ok {
		t.Fatalf("a real dump was refused:\n%s", out)
	}
	info, err := os.Stat(dumpFile)
	if err != nil {
		t.Fatalf("the compressed dump was not written: %v", err)
	}
	if info.Size() == 0 {
		t.Error("the compressed dump is empty")
	}
	if _, err := os.Stat(dumpFile + ".raw"); !os.IsNotExist(err) {
		t.Error("the uncompressed dump was left behind, which doubles the disk a backup needs")
	}
}
