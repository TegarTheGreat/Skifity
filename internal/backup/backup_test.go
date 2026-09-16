package backup

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"

	"skifity/internal/dbsvc"
)

func TestParseScheduleAndMatch(t *testing.T) {
	cases := []struct {
		expression string
		at         string
		want       bool
	}{
		{"0 3 * * *", "2026-09-16T03:00:00Z", true},
		{"0 3 * * *", "2026-09-16T03:01:00Z", false},
		{"0 3 * * *", "2026-09-16T04:00:00Z", false},
		{"*/15 * * * *", "2026-09-16T10:30:00Z", true},
		{"*/15 * * * *", "2026-09-16T10:31:00Z", false},
		{"30 2 * * 0", "2026-09-20T02:30:00Z", true},  // a Sunday
		{"30 2 * * 0", "2026-09-21T02:30:00Z", false}, // a Monday
		{"0 0 1 * *", "2026-10-01T00:00:00Z", true},
		{"0 0 1 * *", "2026-10-02T00:00:00Z", false},
		{"0 9-17 * * 1-5", "2026-09-16T13:00:00Z", true}, // a Wednesday
		{"0 9-17 * * 1-5", "2026-09-19T13:00:00Z", false},
		{"0 9-17 * * 1-5", "2026-09-16T18:00:00Z", false},
		// Sunday is both 0 and 7.
		{"0 1 * * 7", "2026-09-20T01:00:00Z", true},
	}
	for _, tc := range cases {
		schedule, err := ParseSchedule(tc.expression)
		if err != nil {
			t.Fatalf("ParseSchedule(%q): %v", tc.expression, err)
		}
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatalf("parse time: %v", err)
		}
		if got := schedule.Matches(at); got != tc.want {
			t.Errorf("%q at %s matched=%v, want %v", tc.expression, tc.at, got, tc.want)
		}
	}
}

func TestDayOfMonthAndWeekdayAreOred(t *testing.T) {
	// Cron's one genuine oddity: with both day fields restricted, either
	// matching is enough. Getting this wrong means a backup runs far more or
	// far less often than the user asked for.
	schedule, err := ParseSchedule("0 0 1 * 0")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	firstOfMonth, _ := time.Parse(time.RFC3339, "2026-10-01T00:00:00Z") // a Thursday
	sunday, _ := time.Parse(time.RFC3339, "2026-10-04T00:00:00Z")
	neither, _ := time.Parse(time.RFC3339, "2026-10-06T00:00:00Z")

	if !schedule.Matches(firstOfMonth) {
		t.Error("the first of the month did not match")
	}
	if !schedule.Matches(sunday) {
		t.Error("Sunday did not match")
	}
	if schedule.Matches(neither) {
		t.Error("a day that is neither matched")
	}
}

func TestParseScheduleRejectsNonsense(t *testing.T) {
	for _, bad := range []string{
		"", "0 3 * *", "0 3 * * * *", "60 3 * * *", "0 25 * * *",
		"0 3 32 * *", "0 3 * 13 *", "0 3 * * 8", "x 3 * * *", "0 3 5-1 * *", "*/0 * * * *",
	} {
		if _, err := ParseSchedule(bad); err == nil {
			t.Errorf("ParseSchedule(%q) was accepted", bad)
		}
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]string{
		"0 3 * * *":  "every day at 03:00 UTC",
		"30 2 * * 0": "every Sunday at 02:30 UTC",
		"0 * * * *":  "every hour at 00 minutes past",
	}
	for expression, want := range cases {
		schedule, err := ParseSchedule(expression)
		if err != nil {
			t.Fatalf("ParseSchedule(%q): %v", expression, err)
		}
		if got := schedule.Describe(); got != want {
			t.Errorf("Describe(%q) = %q, want %q", expression, got, want)
		}
	}
}

func TestNextRun(t *testing.T) {
	after, _ := time.Parse(time.RFC3339, "2026-09-16T10:15:00Z")
	next, err := NextRun("0 3 * * *", after)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-09-17T03:00:00Z")
	if !next.Equal(want) {
		t.Fatalf("NextRun = %s, want %s", next, want)
	}
}

func TestObjectKeyIsSortableAndSafe(t *testing.T) {
	at, _ := time.Parse(time.RFC3339, "2026-09-16T03:00:00Z")
	key := ObjectKey("database", "db_abc", "My Shop DB!", at)

	if !strings.HasPrefix(key, "skifity/database/db_abc/") {
		t.Fatalf("key is %q", key)
	}
	// Timestamp first inside the folder, so a bucket listing sorts by age.
	if !strings.Contains(key, "20260916-030000") {
		t.Fatalf("key has no sortable timestamp: %q", key)
	}
	if strings.ContainsAny(key, " !") {
		t.Fatalf("the name was not sanitised: %q", key)
	}
	if !strings.HasSuffix(key, ".gz") {
		t.Fatalf("key does not say it is compressed: %q", key)
	}
}

func TestSplitEndpoint(t *testing.T) {
	cases := []struct {
		endpoint string
		host     string
		secure   bool
	}{
		{"https://s3.eu-central-1.amazonaws.com", "s3.eu-central-1.amazonaws.com", true},
		{"http://minio.default.svc:9000", "minio.default.svc:9000", false},
		// A bare host defaults to TLS: guessing plaintext would silently send
		// credentials in the clear.
		{"s3.example.com", "s3.example.com", true},
	}
	for _, tc := range cases {
		host, secure, err := splitEndpoint(tc.endpoint)
		if err != nil {
			t.Errorf("splitEndpoint(%q): %v", tc.endpoint, err)
			continue
		}
		if host != tc.host || secure != tc.secure {
			t.Errorf("splitEndpoint(%q) = %q/%v, want %q/%v", tc.endpoint, host, secure, tc.host, tc.secure)
		}
	}
}

func backupSpec(engine string, restore bool) JobSpec {
	return JobSpec{
		Name: "backup-main-abc", Namespace: "acme-shop-production",
		Engine: engine, CredentialsSecret: "main-credentials",
		URLSecret: "backup-main-abc-url", BackupID: "bak_1", Restore: restore,
	}
}

func TestBackupJobKeepsCredentialsOutOfTheSpec(t *testing.T) {
	for _, engine := range []string{dbsvc.EnginePostgres, dbsvc.EngineMySQL, dbsvc.EngineRedis} {
		job, err := BuildJob(backupSpec(engine, false))
		if err != nil {
			t.Fatalf("%s: BuildJob: %v", engine, err)
		}
		container := job.Spec.Template.Spec.Containers[0]

		// Every value must come from a Secret. A presigned URL is a credential
		// too: it grants bucket access until it expires.
		for _, env := range container.Env {
			if env.Value != "" {
				t.Errorf("%s: %s is a literal value in the Job spec", engine, env.Name)
			}
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Errorf("%s: %s does not come from a Secret", engine, env.Name)
			}
		}
		script := container.Args[0]
		if strings.Contains(script, "https://") && !strings.Contains(script, "$BACKUP_URL") {
			t.Errorf("%s: a URL appears literally in the script", engine)
		}
	}
}

func TestBackupJobStreamsWithoutTouchingDisk(t *testing.T) {
	// A dump written to disk first needs as much free space as the database,
	// inside a container that usually has none.
	job, _ := BuildJob(backupSpec(dbsvc.EnginePostgres, false))
	script := job.Spec.Template.Spec.Containers[0].Args[0]

	if !strings.Contains(script, "| gzip -c | curl") {
		t.Fatalf("the dump is not streamed straight to storage:\n%s", script)
	}
	if !strings.Contains(script, "--upload-file -") {
		t.Fatal("curl is not reading the upload from stdin")
	}
	if !strings.Contains(script, "--fail") {
		t.Fatal("curl would report success on an HTTP error, so a failed upload would look like a good backup")
	}
}

func TestPostgresDumpIsRestorable(t *testing.T) {
	job, _ := BuildJob(backupSpec(dbsvc.EnginePostgres, false))
	script := job.Spec.Template.Spec.Containers[0].Args[0]
	// Without --clean --if-exists, restoring over an existing database fails
	// on every object that already exists.
	if !strings.Contains(script, "--clean") || !strings.Contains(script, "--if-exists") {
		t.Fatalf("the dump could not be restored over an existing database:\n%s", script)
	}
	if !strings.Contains(script, "--no-owner") {
		t.Fatal("the dump records ownership, which breaks a restore into a different role")
	}
}

func TestMySQLDumpIsConsistent(t *testing.T) {
	job, _ := BuildJob(backupSpec(dbsvc.EngineMySQL, false))
	script := job.Spec.Template.Spec.Containers[0].Args[0]
	// Without a single transaction, a dump of a live database is inconsistent
	// between tables.
	if !strings.Contains(script, "--single-transaction") {
		t.Fatalf("the dump is not consistent:\n%s", script)
	}
}

func TestRestoreScriptsInvert(t *testing.T) {
	for _, engine := range []string{dbsvc.EnginePostgres, dbsvc.EngineMySQL, dbsvc.EngineRedis} {
		job, err := BuildJob(backupSpec(engine, true))
		if err != nil {
			t.Fatalf("%s: BuildJob: %v", engine, err)
		}
		script := job.Spec.Template.Spec.Containers[0].Args[0]
		if !strings.Contains(script, "gzip -dc") {
			t.Errorf("%s: the restore does not decompress", engine)
		}
		if !strings.Contains(script, "curl") || !strings.Contains(script, "$BACKUP_URL") {
			t.Errorf("%s: the restore does not download the backup", engine)
		}
	}
	// PostgreSQL must stop at the first error, or a half-restored database
	// looks like a success.
	job, _ := BuildJob(backupSpec(dbsvc.EnginePostgres, true))
	if !strings.Contains(job.Spec.Template.Spec.Containers[0].Args[0], "ON_ERROR_STOP=on") {
		t.Fatal("a failed statement during a restore would be ignored")
	}
}

func TestBackupJobDoesNotRetryBlindly(t *testing.T) {
	job, _ := BuildJob(backupSpec(dbsvc.EnginePostgres, false))
	if *job.Spec.BackoffLimit != 0 {
		t.Fatalf("backoffLimit is %d; a retry would upload to an expired URL", *job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil {
		t.Fatal("a backup with no deadline could run forever")
	}
	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil ||
		*job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("the backup pod mounts an API token it does not need")
	}
}

func TestBuildJobValidates(t *testing.T) {
	cases := map[string]func(*JobSpec){
		"no name":        func(s *JobSpec) { s.Name = "" },
		"no credentials": func(s *JobSpec) { s.CredentialsSecret = "" },
		"no url":         func(s *JobSpec) { s.URLSecret = "" },
		"bad engine":     func(s *JobSpec) { s.Engine = "mongo" },
	}
	for name, mutate := range cases {
		spec := backupSpec(dbsvc.EnginePostgres, false)
		mutate(&spec)
		if _, err := BuildJob(spec); err == nil {
			t.Errorf("%s: an invalid spec was accepted", name)
		}
	}
}

func TestGeneratedBackupScriptsAreValidShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available")
	}
	for _, engine := range []string{dbsvc.EnginePostgres, dbsvc.EngineMySQL, dbsvc.EngineRedis} {
		for _, restore := range []bool{false, true} {
			job, err := BuildJob(backupSpec(engine, restore))
			if err != nil {
				t.Fatalf("%s: BuildJob: %v", engine, err)
			}
			checkShell(t, engine, job)
		}
	}
}

func checkShell(t *testing.T, name string, job *batchv1.Job) {
	t.Helper()
	script := job.Spec.Template.Spec.Containers[0].Args[0]
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("%s is not valid shell: %v\n%s\n---\n%s", name, err, output, script)
	}
}

func TestJobName(t *testing.T) {
	name := JobName("backup-main", "bak_06gapwgqx49m4r2v66bk")
	if len(name) > 63 {
		t.Fatalf("job name is %d characters: %q", len(name), name)
	}
	if !strings.HasPrefix(name, "backup-main-") {
		t.Fatalf("job name is %q", name)
	}
	// Two backups of the same database must not collide.
	if name == JobName("backup-main", "bak_differentidhere") {
		t.Fatal("two backups produced the same job name")
	}
}
