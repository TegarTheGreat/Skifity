package builder

import (
	batchv1 "k8s.io/api/batch/v1"
	"os/exec"
	"strings"
	"testing"
)

func baseJob() JobSpec {
	return JobSpec{
		Name:             "build-dep-1",
		Namespace:        "acme-shop-production",
		AppID:            "app_1",
		DeploymentID:     "dep_1",
		RepoURL:          "https://github.com/acme/shop",
		CommitSHA:        "a1b2c3d4e5f6a7b8",
		Builder:          BuilderRailpack,
		Image:            "skifity-registry.skifity-system.svc.cluster.local:5000/acme-shop-production/web:a1b2c3d",
		RegistryInsecure: true,
		BuildKitAddress:  "tcp://skifity-buildkit.skifity-system.svc.cluster.local:1234",
	}
}

func TestBuildJobStructure(t *testing.T) {
	job, err := BuildJob(baseJob())
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}
	pod := job.Spec.Template.Spec

	// Clone first, then prepare, then build.
	if len(pod.InitContainers) != 2 {
		t.Fatalf("got %d init containers, want clone and prepare", len(pod.InitContainers))
	}
	if pod.InitContainers[0].Name != "clone" || pod.InitContainers[1].Name != "prepare" {
		t.Fatalf("init containers are %s and %s", pod.InitContainers[0].Name, pod.InitContainers[1].Name)
	}
	if len(pod.Containers) != 1 || pod.Containers[0].Name != "build" {
		t.Fatalf("containers are %+v", pod.Containers)
	}

	// A failed build must be reported, not retried blindly: the second attempt
	// fails the same way and doubles the time before the user sees why.
	if *job.Spec.BackoffLimit != 0 {
		t.Fatalf("backoffLimit is %d, want 0", *job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds == 0 {
		t.Fatal("a build with no deadline can occupy the cluster forever")
	}
	// The logs must survive long enough to be read after the build finishes.
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished < 600 {
		t.Fatal("a finished build is cleaned up too quickly to read its logs")
	}
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("the build pod mounts a Kubernetes API token it does not need")
	}
	if pod.Containers[0].Resources.Limits.Memory().IsZero() {
		t.Fatal("a build with no memory limit can take a node down")
	}

	// BUILDKIT_HOST is how buildctl finds the shared builder.
	var sawHost bool
	for _, env := range pod.Containers[0].Env {
		if env.Name == "BUILDKIT_HOST" && env.Value == baseJob().BuildKitAddress {
			sawHost = true
		}
	}
	if !sawHost {
		t.Fatal("BUILDKIT_HOST is not set, so buildctl would look for a local daemon")
	}
}

func TestCloneIsShallowAndPinned(t *testing.T) {
	job, err := BuildJob(baseJob())
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}
	script := job.Spec.Template.Spec.InitContainers[0].Args[0]

	if !strings.Contains(script, "--depth 1") {
		t.Fatal("the clone is not shallow; a large repository would take minutes")
	}
	if !strings.Contains(script, `git fetch --depth 1 -q origin "$GIT_REF"`) {
		t.Fatalf("the ref is not fetched from the environment:\n%s", script)
	}
	if got := cloneEnv(t, job, "GIT_REF"); got != "a1b2c3d4e5f6a7b8" {
		t.Fatalf("GIT_REF is %q, want the exact commit", got)
	}
	if !strings.Contains(script, "git checkout -q FETCH_HEAD") {
		t.Fatal("the fetched commit is not checked out")
	}
}

// cloneEnv reads one environment variable off the clone container.
func cloneEnv(t *testing.T, job *batchv1.Job, name string) string {
	t.Helper()
	for _, env := range job.Spec.Template.Spec.InitContainers[0].Env {
		if env.Name == name {
			return env.Value
		}
	}
	t.Fatalf("the clone container has no %s", name)
	return ""
}

// TestCloneScriptCannotBeInjected: the address and the ref are things a user
// types, and they used to be pasted into a shell script that runs in the build
// pod with the team's Git token in its environment.
func TestCloneScriptCannotBeInjected(t *testing.T) {
	spec := baseJob()
	// A double quote was the one break-out character the old check let past,
	// and the address was pasted between two of them.
	spec.RepoURL = `https://example.test/r.git`
	spec.Branch = `main"; id; echo "`
	spec.CommitSHA = ""
	spec.CloneSecret = "github-token"

	job, err := BuildJob(spec)
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}
	script := job.Spec.Template.Spec.InitContainers[0].Args[0]

	if strings.Contains(script, "example.test") {
		t.Fatalf("the repository address reached the script:\n%s", script)
	}
	if strings.Contains(script, "id;") {
		t.Fatalf("the branch reached the script:\n%s", script)
	}
	// Both belong in the environment, where a shell never re-reads them.
	if got := cloneEnv(t, job, "REPO_URL"); got != spec.RepoURL {
		t.Fatalf("REPO_URL is %q, want the address unchanged", got)
	}
	if got := cloneEnv(t, job, "GIT_REF"); got != spec.Branch {
		t.Fatalf("GIT_REF is %q, want the branch unchanged", got)
	}
}

func TestCloneTokenNeverAppearsInTheSpec(t *testing.T) {
	spec := baseJob()
	spec.CloneSecret = "github-token"
	job, err := BuildJob(spec)
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}

	clone := job.Spec.Template.Spec.InitContainers[0]
	// The token must arrive from a Secret at runtime. Anyone who can read a Job
	// in the namespace could otherwise read the credential out of it.
	var fromSecret bool
	for _, env := range clone.Env {
		if env.Name == "GIT_TOKEN" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			fromSecret = true
			if env.ValueFrom.SecretKeyRef.Name != "github-token" {
				t.Fatalf("the wrong secret is referenced: %s", env.ValueFrom.SecretKeyRef.Name)
			}
		}
		if env.Value != "" && strings.Contains(env.Value, "token") {
			t.Fatalf("a token-looking value is baked into the spec: %s=%s", env.Name, env.Value)
		}
	}
	if !fromSecret {
		t.Fatal("no credential is provided for a private repository")
	}
	if !strings.Contains(clone.Args[0], "${GIT_TOKEN}") {
		t.Fatal("the clone script does not use the injected token")
	}
}

func TestDockerfileBuildCommand(t *testing.T) {
	spec := baseJob()
	spec.Builder = BuilderDockerfile
	spec.DockerfilePath = "docker/Dockerfile"
	spec.BuildArgs = map[string]string{"NODE_ENV": "production", "API_URL": "https://api.example.com"}

	job, err := BuildJob(spec)
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}
	// No prepare step for a Dockerfile build.
	if len(job.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("got %d init containers for a Dockerfile build, want 1", len(job.Spec.Template.Spec.InitContainers))
	}

	script := job.Spec.Template.Spec.Containers[0].Args[0]
	for _, want := range []string{
		`--frontend dockerfile.v0`,
		`--local "context=/workspace"`,
		`--local "dockerfile=/workspace/docker"`,
		`--opt "filename=Dockerfile"`,
		`--opt "build-arg:API_URL=https://api.example.com"`,
		`--opt "build-arg:NODE_ENV=production"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the build command is missing %s:\n%s", want, script)
		}
	}
	// Build arguments must be in a stable order, or the command differs between
	// identical builds.
	if strings.Index(script, "API_URL") > strings.Index(script, "NODE_ENV") {
		t.Fatal("build arguments are not sorted, so two identical builds produce different commands")
	}
}

func TestRailpackBuildCommand(t *testing.T) {
	job, err := BuildJob(baseJob())
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}

	prepare := job.Spec.Template.Spec.InitContainers[1].Args[0]
	if !strings.Contains(prepare, "railpack prepare") {
		t.Fatalf("the prepare step does not run railpack:\n%s", prepare)
	}
	if !strings.Contains(prepare, "--plan-out /workspace/railpack-plan.json") {
		t.Fatalf("the build plan is not written where the frontend reads it:\n%s", prepare)
	}

	script := job.Spec.Template.Spec.Containers[0].Args[0]
	for _, want := range []string{
		"--frontend gateway.v0",
		`--opt "source=ghcr.io/railwayapp/railpack-frontend:latest"`,
		`--local "dockerfile=/workspace"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the build command is missing %s:\n%s", want, script)
		}
	}
}

func TestStaticSiteGeneratesItsOwnDockerfile(t *testing.T) {
	spec := baseJob()
	spec.Builder = BuilderStatic
	spec.StaticDir = "dist"

	job, err := BuildJob(spec)
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}
	script := job.Spec.Template.Spec.Containers[0].Args[0]
	if !strings.Contains(script, "FROM caddy:2-alpine") {
		t.Fatalf("no Dockerfile was generated for the static site:\n%s", script)
	}
	if !strings.Contains(script, "COPY dist /srv") {
		t.Fatalf("the configured directory is not copied:\n%s", script)
	}
	// SPA routing: without the fallback, every deep link 404s.
	if !strings.Contains(script, "try_files") {
		t.Fatal("no single-page-app fallback, so refreshing a sub-route would 404")
	}
}

func TestRootDirBuildsASubdirectory(t *testing.T) {
	spec := baseJob()
	spec.RootDir = "apps/web"
	job, err := BuildJob(spec)
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}
	script := job.Spec.Template.Spec.Containers[0].Args[0]
	if !strings.Contains(script, `--local "context=/workspace/apps/web"`) {
		t.Fatalf("the monorepo subdirectory is not the build context:\n%s", script)
	}
	prepare := job.Spec.Template.Spec.InitContainers[1].Args[0]
	if !strings.Contains(prepare, `"/workspace/apps/web"`) {
		t.Fatalf("railpack was pointed at the wrong directory:\n%s", prepare)
	}
}

func TestBuildCacheIsUsed(t *testing.T) {
	job, err := BuildJob(baseJob())
	if err != nil {
		t.Fatalf("BuildJob: %v", err)
	}
	script := job.Spec.Template.Spec.Containers[0].Args[0]
	if !strings.Contains(script, "--export-cache") || !strings.Contains(script, "--import-cache") {
		t.Fatal("no build cache, so every deploy rebuilds from scratch")
	}
	if !strings.Contains(script, ":buildcache") {
		t.Fatalf("the cache reference looks wrong:\n%s", script)
	}
}

func TestCacheRef(t *testing.T) {
	cases := map[string]string{
		"registry:5000/ns/app:sha123": "registry:5000/ns/app:buildcache",
		"registry/ns/app":             "registry/ns/app:buildcache",
		"ghcr.io/owner/app:latest":    "ghcr.io/owner/app:buildcache",
	}
	for image, want := range cases {
		if got := cacheRef(image); got != want {
			t.Errorf("cacheRef(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestValidateRejectsDangerousInput(t *testing.T) {
	cases := map[string]func(*JobSpec){
		"no image":        func(s *JobSpec) { s.Image = "" },
		"no builder addr": func(s *JobSpec) { s.BuildKitAddress = "" },
		"no repo":         func(s *JobSpec) { s.RepoURL = "" },
		"prebuilt image":  func(s *JobSpec) { s.Builder = BuilderImage },
		"unknown builder": func(s *JobSpec) { s.Builder = Builder("magic") },
		"bad commit":      func(s *JobSpec) { s.CommitSHA = "not-a-hash" },
		// The URL ends up in a shell command.
		"injected url": func(s *JobSpec) { s.RepoURL = "https://x/y; rm -rf /" },
		"backtick url": func(s *JobSpec) { s.RepoURL = "https://x/`whoami`" },
	}
	for name, mutate := range cases {
		spec := baseJob()
		mutate(&spec)
		if _, err := BuildJob(spec); err == nil {
			t.Errorf("%s: BuildJob accepted an invalid spec", name)
		}
	}
}

func TestGeneratedBuildScriptsAreValidShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available")
	}
	for _, builder := range []Builder{BuilderRailpack, BuilderDockerfile, BuilderStatic, BuilderNixpacks} {
		spec := baseJob()
		spec.Builder = builder
		spec.StaticDir = "dist"
		spec.BuildArgs = map[string]string{"A": "1"}
		job, err := BuildJob(spec)
		if err != nil {
			t.Fatalf("%s: BuildJob: %v", builder, err)
		}
		scripts := map[string]string{
			"clone": job.Spec.Template.Spec.InitContainers[0].Args[0],
			"build": job.Spec.Template.Spec.Containers[0].Args[0],
		}
		if len(job.Spec.Template.Spec.InitContainers) > 1 {
			scripts["prepare"] = job.Spec.Template.Spec.InitContainers[1].Args[0]
		}
		for name, script := range scripts {
			cmd := exec.Command("sh", "-n")
			cmd.Stdin = strings.NewReader(script)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s/%s is not valid shell: %v\n%s\n---\n%s", builder, name, err, output, script)
			}
		}
	}
}

func TestImageName(t *testing.T) {
	got := ImageName("registry:5000", "acme-shop-production", "web", "a1b2c3d")
	if got != "registry:5000/acme-shop-production/web:a1b2c3d" {
		t.Fatalf("ImageName = %q", got)
	}
	// A trailing slash on the registry must not produce a double slash.
	if got := ImageName("registry:5000/", "ns", "app", ""); got != "registry:5000/ns/app:latest" {
		t.Fatalf("ImageName with a trailing slash = %q", got)
	}
}
