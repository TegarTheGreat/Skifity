package deploy

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"skifity/internal/api"
	"skifity/internal/crypto"
	"skifity/internal/events"
	"skifity/internal/store"
)

func testDeployer(t *testing.T) (*Deployer, *store.DB, store.App, store.Environment) {
	t.Helper()

	db, err := store.OpenMemory(t.Context())
	if err != nil {
		t.Fatalf("open the test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	key, _ := crypto.GenerateKey()
	keyring, err := crypto.NewKeyring("k1", key)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}

	team := store.Team{Name: "Acme", Slug: "acme"}
	if err := db.CreateTeam(t.Context(), &team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project := store.Project{TeamID: team.ID, Name: "Shop", Slug: "shop"}
	if err := db.CreateProject(t.Context(), &project); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	env := store.Environment{
		ProjectID: project.ID, Name: "Production", Slug: "production",
		Namespace: "acme-shop-production",
	}
	if err := db.CreateEnvironment(t.Context(), &env); err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	app := store.App{
		EnvironmentID: env.ID, Name: "Web", Slug: "web", SourceType: "git",
		RepoURL: "https://github.com/acme/shop", Branch: "main", Builder: "auto",
		Port: 3000, Replicas: 1, MinReplicas: 1, MaxReplicas: 3, CPUTarget: 75,
		CPURequestM: 50, CPULimitM: 1000, MemRequestMB: 128, MemLimitMB: 512,
	}
	if err := db.CreateApp(t.Context(), &app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// No cluster: this exercises the decision logic, which is where the
	// interesting behaviour lives.
	d := New(db, keyring, events.NewHub(64), nil, nil, logger)
	return d, db, app, env
}

// markBuilt waits for a deployment's background run to finish, then records it
// as a successful build. Without the wait, the background run would overwrite
// the status a moment after the test sets it.
func markBuilt(t *testing.T, db *store.DB, deploymentID, image string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		deployment, err := db.GetDeployment(t.Context(), deploymentID)
		if err != nil {
			t.Fatalf("GetDeployment: %v", err)
		}
		if deployment.Status.Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the deployment was still %s after 10 seconds", deployment.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := db.SetDeploymentImage(t.Context(), deploymentID, image); err != nil {
		t.Fatalf("SetDeploymentImage: %v", err)
	}
	if err := db.UpdateDeploymentStatus(t.Context(), deploymentID, store.DeploySucceeded, "", "", ""); err != nil {
		t.Fatalf("UpdateDeploymentStatus: %v", err)
	}
}

func setVariable(t *testing.T, d *Deployer, db *store.DB, appID, key, value string, buildTime bool) {
	t.Helper()
	sealed, err := d.keyring.Seal([]byte(value), "variable:"+appID+":"+key)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	v := store.Variable{AppID: appID, Key: key, BuildTime: buildTime}
	if err := db.SetVariable(t.Context(), &v, sealed); err != nil {
		t.Fatalf("SetVariable: %v", err)
	}
}

func TestRuntimeChangeReusesTheExistingImage(t *testing.T) {
	// This is the fix for the most common complaint about comparable products:
	// changing a setting must not trigger a rebuild.
	d, db, app, _ := testDeployer(t)

	first, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "abc123def456"})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	markBuilt(t, db, first.ID, "registry/acme/web:abc123def456")

	// A runtime-only variable changes nothing about the image.
	setVariable(t, d, db, app.ID, "LOG_LEVEL", "debug", false)

	second, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "abc123def456"})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if second.Image != "registry/acme/web:abc123def456" {
		t.Fatalf("the second deploy did not reuse the image: %q", second.Image)
	}
	if second.BuildFingerprint != first.BuildFingerprint {
		t.Fatal("a runtime variable changed the build fingerprint")
	}
}

func TestBuildTimeVariableForcesARebuild(t *testing.T) {
	d, db, app, _ := testDeployer(t)

	first, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "abc123def456"})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	markBuilt(t, db, first.ID, "registry/acme/web:1")

	// A build-time variable is baked into the image, so it must rebuild.
	setVariable(t, d, db, app.ID, "NEXT_PUBLIC_API_URL", "https://api.example.com", true)

	second, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "abc123def456"})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if second.BuildFingerprint == first.BuildFingerprint {
		t.Fatal("a build-time variable did not change the fingerprint, so the stale image would be reused")
	}
	if second.Image != "" {
		t.Fatalf("a rebuild was expected, but an image was reused: %q", second.Image)
	}
}

func TestNewCommitRebuilds(t *testing.T) {
	d, db, app, _ := testDeployer(t)

	first, _ := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "aaaaaaaaaaaa"})
	markBuilt(t, db, first.ID, "registry/acme/web:aaa")

	second, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "bbbbbbbbbbbb"})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if second.Image != "" {
		t.Fatalf("a new commit reused the old image: %q", second.Image)
	}
}

func TestForceRebuilds(t *testing.T) {
	d, db, app, _ := testDeployer(t)

	first, _ := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "abcabcabcabc"})
	markBuilt(t, db, first.ID, "registry/acme/web:abc")

	forced, err := d.Deploy(t.Context(), api.DeployRequest{
		AppID: app.ID, CommitSHA: "abcabcabcabc", Force: true,
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if forced.Image != "" {
		t.Fatal("a forced deploy reused the existing image")
	}
}

func TestPrebuiltImageNeverBuilds(t *testing.T) {
	d, db, _, env := testDeployer(t)

	app := store.App{
		EnvironmentID: env.ID, Name: "Cache", Slug: "cache",
		SourceType: "image", Image: "redis:7", Port: 6379, Replicas: 1,
	}
	if err := db.CreateApp(t.Context(), &app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	deployment, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if deployment.Image != "redis:7" {
		t.Fatalf("the prebuilt image is %q, want redis:7", deployment.Image)
	}
	if !strings.HasPrefix(deployment.BuildFingerprint, "image:") {
		t.Fatalf("fingerprint is %q; a prebuilt image is its own fingerprint", deployment.BuildFingerprint)
	}
}

func TestDeploySupersedesEarlierAttempts(t *testing.T) {
	d, db, app, _ := testDeployer(t)

	first, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "aaaaaaaaaaaa"})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if _, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "bbbbbbbbbbbb"}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	// Two builds racing for the same app would deploy in an unpredictable
	// order, so the older one must not be left looking live.
	reloaded, err := db.GetDeployment(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if !reloaded.Status.Terminal() {
		t.Fatalf("the earlier deployment is %s, so two deploys would race", reloaded.Status)
	}
}

func TestRuntimeSpecIsRecordedForRollback(t *testing.T) {
	d, db, app, _ := testDeployer(t)

	if err := db.CreateDomain(t.Context(), &store.Domain{
		AppID: app.ID, Hostname: "shop.example.com", TLS: true, Path: "/",
	}); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}

	deployment, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "aaaaaaaaaaaa"})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	// A rollback that restores only the image would leave a bad configuration
	// change in place, so the settings are recorded too.
	for _, needed := range []string{`"replicas":1`, `"port":3000`, "shop.example.com"} {
		if !strings.Contains(deployment.RuntimeSpec, needed) {
			t.Errorf("the runtime spec is missing %s: %s", needed, deployment.RuntimeSpec)
		}
	}
}

func TestRollbackRestoresSettings(t *testing.T) {
	d, db, app, _ := testDeployer(t)

	// Deploy with two instances.
	app.Replicas = 2
	if err := db.UpdateApp(t.Context(), &app); err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	good, err := d.Deploy(t.Context(), api.DeployRequest{AppID: app.ID, CommitSHA: "aaaaaaaaaaaa"})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	markBuilt(t, db, good.ID, "registry/acme/web:good")

	// Then somebody changes the settings badly.
	app.Replicas = 1
	app.Port = 9999
	if err := db.UpdateApp(t.Context(), &app); err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}

	rolled, err := d.Rollback(t.Context(), app.ID, good.ID, "usr_1")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rolled.Image != "registry/acme/web:good" {
		t.Fatalf("the rollback did not carry the old image: %q", rolled.Image)
	}
	if !strings.Contains(rolled.Trigger, "rollback") {
		t.Fatalf("the trigger is %q", rolled.Trigger)
	}

	restored, err := db.GetApp(t.Context(), app.ID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if restored.Replicas != 2 || restored.Port != 3000 {
		t.Fatalf("the settings were not restored: %d instances, port %d", restored.Replicas, restored.Port)
	}
}

func TestRollbackRefusesAnotherAppsDeployment(t *testing.T) {
	d, db, app, env := testDeployer(t)

	other := store.App{EnvironmentID: env.ID, Name: "Other", Slug: "other", SourceType: "image", Image: "nginx"}
	if err := db.CreateApp(t.Context(), &other); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	theirs, err := d.Deploy(t.Context(), api.DeployRequest{AppID: other.ID})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if _, err := d.Rollback(t.Context(), app.ID, theirs.ID, "usr_1"); err == nil {
		t.Fatal("one app was rolled back to another app's deployment")
	}
}

func TestChooseBuilder(t *testing.T) {
	d, _, _, _ := testDeployer(t)
	cases := []struct {
		setting    string
		dockerfile string
		want       string
	}{
		{"auto", "", "railpack"},
		{"auto", "Dockerfile", "dockerfile"},
		{"", "", "railpack"},
		{"dockerfile", "", "dockerfile"},
		{"nixpacks", "", "nixpacks"},
		{"static", "", "static"},
	}
	for _, tc := range cases {
		got, err := d.chooseBuilder(store.App{Builder: tc.setting, DockerfilePath: tc.dockerfile})
		if err != nil {
			t.Fatalf("chooseBuilder(%q): %v", tc.setting, err)
		}
		if string(got) != tc.want {
			t.Errorf("chooseBuilder(%q, dockerfile=%q) = %q, want %q",
				tc.setting, tc.dockerfile, got, tc.want)
		}
	}
	if _, err := d.chooseBuilder(store.App{Builder: "magic"}); err == nil {
		t.Fatal("an unknown builder was accepted")
	}
}

func TestScalingReadinessFindsRealProblems(t *testing.T) {
	d, db, app, _ := testDeployer(t)

	// A volume, SQLite, in-memory sessions and local uploads: the four ways an
	// app usually breaks when it is scaled.
	if err := db.CreateVolume(t.Context(), &store.Volume{
		AppID: app.ID, Name: "data", MountPath: "/data", SizeGB: 5,
	}); err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	setVariable(t, d, db, app.ID, "DATABASE_URL", "sqlite:///data/app.db", false)
	setVariable(t, d, db, app.ID, "SESSION_DRIVER", "memory", false)
	setVariable(t, d, db, app.ID, "UPLOAD_DIR", "/data/uploads", false)

	findings, err := d.ScalingReadiness(t.Context(), app.ID)
	if err != nil {
		t.Fatalf("ScalingReadiness: %v", err)
	}

	byCode := map[string]api.ScalingFinding{}
	for _, f := range findings {
		byCode[f.Code] = f
	}
	for _, code := range []string{"volume", "sqlite", "in_memory_sessions", "local_uploads"} {
		finding, ok := byCode[code]
		if !ok {
			t.Errorf("%s was not detected; findings were %v", code, keys(byCode))
			continue
		}
		if finding.Fix == "" {
			t.Errorf("%s has no suggested fix", code)
		}
		if finding.Severity != "error" {
			t.Errorf("%s has severity %q, want error", code, finding.Severity)
		}
	}
}

func TestScalingReadinessIsQuietForAGoodApp(t *testing.T) {
	d, db, app, _ := testDeployer(t)
	app.HealthPath = "/healthz"
	app.Replicas = 2
	if err := db.UpdateApp(t.Context(), &app); err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	setVariable(t, d, db, app.ID, "DATABASE_URL", "postgres://user@db:5432/app", false)
	setVariable(t, d, db, app.ID, "REDIS_URL", "redis://cache:6379", false)

	findings, err := d.ScalingReadiness(t.Context(), app.ID)
	if err != nil {
		t.Fatalf("ScalingReadiness: %v", err)
	}
	// A stateless app on Postgres and Redis with a health check is exactly
	// what the checker should say nothing about.
	for _, f := range findings {
		if f.Severity == "error" {
			t.Errorf("a well-configured app produced an error finding: %s - %s", f.Code, f.Detail)
		}
	}
}

func TestScalingReadinessDoesNotDecryptSecrets(t *testing.T) {
	d, db, app, _ := testDeployer(t)

	sealed, err := d.keyring.Seal([]byte("sqlite:///secret.db"), "variable:"+app.ID+":SESSION_SECRET")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	v := store.Variable{AppID: app.ID, Key: "SESSION_SECRET", IsSecret: true}
	if err := db.SetVariable(t.Context(), &v, sealed); err != nil {
		t.Fatalf("SetVariable: %v", err)
	}

	findings, err := d.ScalingReadiness(t.Context(), app.ID)
	if err != nil {
		t.Fatalf("ScalingReadiness: %v", err)
	}
	// The heuristics must never put a secret's value into a message shown in
	// the UI and copied into an AI prompt.
	for _, f := range findings {
		if strings.Contains(f.Detail, "secret.db") {
			t.Fatalf("a secret's value leaked into a finding: %s", f.Detail)
		}
	}
}

func TestSingleInstanceIsInfoNotError(t *testing.T) {
	d, _, app, _ := testDeployer(t)
	findings, err := d.ScalingReadiness(t.Context(), app.ID)
	if err != nil {
		t.Fatalf("ScalingReadiness: %v", err)
	}
	for _, f := range findings {
		if f.Code == "single_instance" && f.Severity != "info" {
			t.Fatalf("running one instance is reported as %q, which is too loud", f.Severity)
		}
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
