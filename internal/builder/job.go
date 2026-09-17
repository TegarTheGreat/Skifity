package builder

import (
	"fmt"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"skifity/internal/version"
)

// Building in the cluster.
//
// A build is a Kubernetes Job with three phases in one pod:
//
//  1. an init container clones the repository at an exact commit;
//  2. for the zero-config builder, a second init container runs
//     `railpack prepare` to produce a build plan;
//  3. the main container runs buildctl against the shared BuildKit service and
//     pushes the result to the in-cluster registry.
//
// Nothing here needs Docker on the host, and nothing runs privileged: BuildKit
// is rootless and lives in its own pod.

// JobSpec describes one build.
type JobSpec struct {
	// Identity
	Name         string
	Namespace    string
	AppID        string
	DeploymentID string

	// Source
	RepoURL   string
	CommitSHA string
	Branch    string
	// RootDir is the subdirectory to build, for a monorepo.
	RootDir string
	// CloneSecret holds credentials for a private repository.
	CloneSecret string

	// Strategy
	Builder        Builder
	DockerfilePath string
	StaticDir      string

	// Output
	Image string
	// RegistryInsecure is true for the in-cluster registry, which speaks plain
	// HTTP inside the cluster network.
	RegistryInsecure bool
	// RegistrySecret holds credentials for an external registry.
	RegistrySecret string

	// BuildArgs are build-time variables, which are part of the fingerprint.
	BuildArgs map[string]string

	// BuildKitAddress is the shared builder, for example
	// tcp://skifity-buildkit.skifity-system.svc.cluster.local:1234.
	BuildKitAddress string

	// Images used by the build itself, overridable for an air-gapped install.
	GitImage      string
	BuildKitImage string
	RailpackImage string
	// RailpackFrontend is the BuildKit gateway frontend image.
	RailpackFrontend string
	// NixpacksImage generates a Dockerfile for the fallback builder.
	NixpacksImage string

	// Resources for the build pod.
	CPURequestM  int
	MemRequestMB int
	MemLimitMB   int

	// TimeoutSeconds bounds the whole build.
	TimeoutSeconds int
}

// Defaults fills in the images and limits a caller did not set.
func (s *JobSpec) Defaults() {
	if s.GitImage == "" {
		s.GitImage = "alpine/git:latest"
	}
	if s.BuildKitImage == "" {
		s.BuildKitImage = "moby/buildkit:master"
	}
	if s.RailpackImage == "" {
		s.RailpackImage = "ghcr.io/railwayapp/railpack:latest"
	}
	if s.RailpackFrontend == "" {
		s.RailpackFrontend = "ghcr.io/railwayapp/railpack-frontend:latest"
	}
	if s.NixpacksImage == "" {
		s.NixpacksImage = "ghcr.io/railwayapp/nixpacks:latest"
	}
	if s.CPURequestM == 0 {
		s.CPURequestM = 200
	}
	if s.MemRequestMB == 0 {
		s.MemRequestMB = 512
	}
	if s.MemLimitMB == 0 {
		// Builds are the most memory-hungry thing the cluster does, and a
		// bundler on a large front end will use every megabyte it is given.
		s.MemLimitMB = 3072
	}
	if s.TimeoutSeconds == 0 {
		s.TimeoutSeconds = 30 * 60
	}
	if s.DockerfilePath == "" {
		s.DockerfilePath = "Dockerfile"
	}
}

// Validate reports a spec that could not produce a working build.
func (s JobSpec) Validate() error {
	if s.Name == "" || s.Namespace == "" {
		return fmt.Errorf("a build needs a name and a namespace")
	}
	if s.Image == "" {
		return fmt.Errorf("a build needs an image to push to")
	}
	if s.BuildKitAddress == "" {
		return fmt.Errorf("a build needs the address of the builder")
	}
	switch s.Builder {
	case BuilderDockerfile, BuilderRailpack, BuilderNixpacks, BuilderStatic:
	case BuilderImage:
		return fmt.Errorf("a prebuilt image does not need a build")
	default:
		return fmt.Errorf("%q is not a builder that can produce an image", s.Builder)
	}
	if s.Builder != BuilderStatic && s.RepoURL == "" {
		return fmt.Errorf("a build needs a repository to build from")
	}
	// The address and the ref reach the build pod through the environment, not
	// through the script, so nothing here can change a command's meaning. This
	// is the second lock on the same door: a shell is involved somewhere in
	// every build system, and a repository address is something a user types.
	if strings.ContainsAny(s.RepoURL, " \t\n;&|`$\"'\\<>(){}") {
		return fmt.Errorf("the repository URL contains characters that are not allowed")
	}
	if s.CommitSHA != "" && !isHex(s.CommitSHA) {
		return fmt.Errorf("the commit %q is not a valid hash", s.CommitSHA)
	}
	return nil
}

// workspace is where the repository is cloned inside the build pod.
const workspace = "/workspace"

// BuildJob renders the Kubernetes Job for a build.
func BuildJob(s JobSpec) (*batchv1.Job, error) {
	s.Defaults()
	if err := s.Validate(); err != nil {
		return nil, err
	}

	labels := map[string]string{
		"app.kubernetes.io/name":          s.Name,
		"app.kubernetes.io/managed-by":    version.Binary,
		"app.kubernetes.io/component":     "build",
		version.LabelKey("app-id"):        s.AppID,
		version.LabelKey("deployment-id"): s.DeploymentID,
	}

	volumes := []corev1.Volume{
		{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	mounts := []corev1.VolumeMount{{Name: "workspace", MountPath: workspace}}

	initContainers := []corev1.Container{cloneContainer(s, mounts)}
	switch s.Builder {
	case BuilderRailpack:
		initContainers = append(initContainers, prepareContainer(s, mounts))
	case BuilderNixpacks:
		// Without this the build reads a Dockerfile nothing wrote. The
		// builder was selectable, the buildctl line pointed at
		// .nixpacks/Dockerfile, and no step ever produced one, so every
		// build with it chosen failed on a missing file.
		initContainers = append(initContainers, nixpacksContainer(s, mounts))
	}

	buildContainer := corev1.Container{
		Name:         "build",
		Image:        s.BuildKitImage,
		Command:      []string{"/bin/sh", "-c"},
		Args:         []string{buildScript(s)},
		VolumeMounts: mounts,
		Env: []corev1.EnvVar{
			{Name: "BUILDKIT_HOST", Value: s.BuildKitAddress},
		},
		Resources: buildResources(s),
	}
	if s.RegistrySecret != "" {
		// buildctl reads registry credentials from a Docker config file.
		volumes = append(volumes, corev1.Volume{
			Name: "registry-auth",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s.RegistrySecret,
					Items:      []corev1.KeyToPath{{Key: ".dockerconfigjson", Path: "config.json"}},
				},
			},
		})
		buildContainer.VolumeMounts = append(buildContainer.VolumeMounts,
			corev1.VolumeMount{Name: "registry-auth", MountPath: "/root/.docker", ReadOnly: true})
	}

	backoffLimit := int32(0) // a failed build is reported, not retried blindly
	activeDeadline := int64(s.TimeoutSeconds)
	ttl := int32(3600) // keep a finished build for an hour so its logs can be read

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Name,
			Namespace: s.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &activeDeadline,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					InitContainers:               initContainers,
					Containers:                   []corev1.Container{buildContainer},
					Volumes:                      volumes,
					AutomountServiceAccountToken: ptr(false),
				},
			},
		},
	}, nil
}

func cloneContainer(s JobSpec, mounts []corev1.VolumeMount) corev1.Container {
	container := corev1.Container{
		Name:         "clone",
		Image:        s.GitImage,
		Command:      []string{"/bin/sh", "-c"},
		Args:         []string{cloneScript(s)},
		VolumeMounts: mounts,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
		},
	}
	container.Env = append(container.Env,
		corev1.EnvVar{Name: "REPO_URL", Value: s.RepoURL},
		corev1.EnvVar{Name: "GIT_REF", Value: s.cloneRef()},
	)
	if s.CloneSecret != "" {
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "GIT_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: s.CloneSecret},
					Key:                  "token",
				},
			},
		})
	}
	return container
}

// cloneScript fetches exactly one commit rather than the whole history, which
// on a large repository is the difference between seconds and minutes.
//
// Nothing the user typed is written into this script. The repository address
// and the ref arrive through the environment, because a script is a shell
// program and a repository address that ends a quoted string would otherwise
// be a way to run commands inside the build pod.
func cloneScript(s JobSpec) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("echo '==> Fetching the repository'\n")

	if s.CloneSecret != "" {
		// The token is injected at runtime from the environment, so it never
		// appears in the Job spec, which anyone with read access could see.
		// Only the scheme is replaced, so the host stays exactly as given.
		b.WriteString(`URL=$(printf '%s' "$REPO_URL" | sed "s#^https://#https://x-access-token:${GIT_TOKEN}@#")` + "\n")
	} else {
		b.WriteString(`URL="$REPO_URL"` + "\n")
	}

	b.WriteString("cd " + workspace + "\n")
	b.WriteString("git init -q .\n")
	b.WriteString(`git remote add origin "$URL"` + "\n")
	b.WriteString(`git fetch --depth 1 -q origin "$GIT_REF"` + "\n")
	b.WriteString("git checkout -q FETCH_HEAD\n")
	// Submodules are common enough that failing on them would be surprising.
	b.WriteString("git submodule update --init --recursive --depth 1 -q 2>/dev/null || true\n")
	b.WriteString(`echo "==> Checked out $(git rev-parse --short HEAD)"` + "\n")
	return b.String()
}

// cloneRef is the exact thing to fetch: a commit when there is one, otherwise
// the branch, otherwise whatever the remote calls its default.
func (s JobSpec) cloneRef() string {
	if s.CommitSHA != "" {
		return s.CommitSHA
	}
	if s.Branch != "" {
		return s.Branch
	}
	return "HEAD"
}

// prepareContainer runs `railpack prepare`, which writes the build plan the
// BuildKit frontend reads.
func prepareContainer(s JobSpec, mounts []corev1.VolumeMount) corev1.Container {
	context := workspace
	if s.RootDir != "" {
		context = workspace + "/" + strings.Trim(s.RootDir, "/")
	}

	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("echo '==> Working out how to build this repository'\n")
	fmt.Fprintf(&b, "railpack prepare %q --plan-out %s/railpack-plan.json --info-out %s/railpack-info.json",
		context, workspace, workspace)
	for _, pair := range sortedPairs(s.BuildArgs) {
		// --env is how build-time configuration reaches the detection, which
		// matters for frameworks that build differently per environment.
		fmt.Fprintf(&b, " --env %q", pair[0]+"="+pair[1])
	}
	b.WriteString("\n")
	b.WriteString("echo '==> Build plan ready'\n")

	return corev1.Container{
		Name:         "prepare",
		Image:        s.RailpackImage,
		Command:      []string{"/bin/sh", "-c"},
		Args:         []string{b.String()},
		VolumeMounts: mounts,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
		},
	}
}

// nixpacksContainer generates the Dockerfile the fallback builder builds.
//
// `nixpacks build --out` writes .nixpacks/Dockerfile and the files it needs,
// and does not call Docker, which is the whole reason it can run here: the
// build itself still happens in rootless BuildKit like every other builder.
//
// Railpack is the zero-config builder Skifity uses by default. This one is kept
// because Railpack is young, and an app that will not build with it should have
// somewhere to go that is not "write a Dockerfile".
func nixpacksContainer(s JobSpec, mounts []corev1.VolumeMount) corev1.Container {
	context := workspace
	if s.RootDir != "" {
		context = workspace + "/" + strings.Trim(s.RootDir, "/")
	}

	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("echo '==> Working out how to build this repository'\n")
	fmt.Fprintf(&b, "nixpacks build %q --out %s", context, workspace)
	for _, pair := range sortedPairs(s.BuildArgs) {
		// The same reason as railpack prepare: a framework that builds
		// differently per environment needs these during detection, not only
		// during the build.
		fmt.Fprintf(&b, " --env %q", pair[0]+"="+pair[1])
	}
	b.WriteString("\n")
	b.WriteString("echo '==> Build plan ready'\n")

	return corev1.Container{
		Name:         "prepare",
		Image:        s.NixpacksImage,
		Command:      []string{"/bin/sh", "-c"},
		Args:         []string{b.String()},
		VolumeMounts: mounts,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
		},
	}
}

// buildScript builds the buildctl command line for the chosen builder.
func buildScript(s JobSpec) string {
	context := workspace
	if s.RootDir != "" {
		context = workspace + "/" + strings.Trim(s.RootDir, "/")
	}

	output := fmt.Sprintf("type=image,name=%s,push=true", s.Image)
	if s.RegistryInsecure {
		// The in-cluster registry speaks plain HTTP on the cluster network,
		// which never leaves the cluster.
		output += ",registry.insecure=true"
	}

	// Each entry is a whole flag with its value, so the rendered command has one
	// readable option per line rather than a flag stranded from its argument.
	var args []string
	switch s.Builder {
	case BuilderDockerfile:
		dockerfileDir := context
		filename := s.DockerfilePath
		if idx := strings.LastIndex(s.DockerfilePath, "/"); idx > 0 {
			dockerfileDir = workspace + "/" + s.DockerfilePath[:idx]
			filename = s.DockerfilePath[idx+1:]
		}
		args = []string{
			"--frontend dockerfile.v0",
			flag("--local", "context="+context),
			flag("--local", "dockerfile="+dockerfileDir),
			flag("--opt", "filename="+filename),
		}
		for _, pair := range sortedPairs(s.BuildArgs) {
			args = append(args, flag("--opt", fmt.Sprintf("build-arg:%s=%s", pair[0], pair[1])))
		}

	case BuilderRailpack:
		// The frontend reads railpack-plan.json from the dockerfile context,
		// which is why prepare wrote it to the workspace root.
		args = []string{
			"--frontend gateway.v0",
			flag("--opt", "source="+s.RailpackFrontend),
			flag("--local", "context="+context),
			flag("--local", "dockerfile="+workspace),
		}

	case BuilderNixpacks:
		// Nixpacks generates a Dockerfile, so the dockerfile frontend builds it.
		args = []string{
			"--frontend dockerfile.v0",
			flag("--local", "context="+context),
			flag("--local", "dockerfile="+workspace+"/.nixpacks"),
			flag("--opt", "filename=Dockerfile"),
		}

	case BuilderStatic:
		// A static site needs no build inputs beyond the files themselves, so
		// a generated Dockerfile is simpler and faster than a buildpack.
		args = []string{
			"--frontend dockerfile.v0",
			flag("--local", "context="+context),
			flag("--local", "dockerfile="+workspace+"/.skifity"),
			flag("--opt", "filename=Dockerfile"),
		}
	}

	// Caching between builds is what makes the second deploy fast.
	//
	// The cache is exported to the same tag it is imported from. That sounds
	// obvious, and it is the bug this replaced: the export was "inline", which
	// writes the cache into the image's own manifest, while the import read a
	// :buildcache tag nothing ever wrote. Every build was a cold build, and
	// nothing said so, because a cache miss is not an error.
	cache := fmt.Sprintf("type=registry,ref=%s", cacheRef(s.Image))
	if s.RegistryInsecure {
		// The cache ref needs this as much as the output does: it is the same
		// registry, over the same plain HTTP.
		cache += ",registry.insecure=true"
	}
	args = append(args,
		// mode=max keeps the intermediate layers, which is the difference
		// between caching the final image and caching the work that made it.
		flag("--export-cache", cache+",mode=max"),
		flag("--import-cache", cache),
		flag("--output", output),
		"--progress plain",
	)

	var b strings.Builder
	b.WriteString("set -e\n")
	if s.Builder == BuilderStatic {
		b.WriteString(staticDockerfileScript(s))
	}
	b.WriteString("echo '==> Building the image'\n")
	b.WriteString("buildctl build \\\n  " + strings.Join(args, " \\\n  ") + "\n")
	fmt.Fprintf(&b, "echo '==> Pushed %s'\n", s.Image)
	return b.String()
}

// staticDockerfileScript writes the Dockerfile used for a static site.
func staticDockerfileScript(s JobSpec) string {
	dir := s.StaticDir
	if dir == "" {
		dir = "."
	}
	// Caddy rather than nginx: it needs no configuration to serve a directory
	// with correct MIME types and SPA fallback, and its image is smaller.
	dockerfile := fmt.Sprintf(`FROM caddy:2-alpine
COPY %s /srv
RUN printf ':80 {\n  root * /srv\n  file_server\n  try_files {path} /index.html\n  encode gzip\n}\n' > /etc/caddy/Caddyfile
EXPOSE 80
`, dir)

	return fmt.Sprintf(`mkdir -p %s/.skifity
cat > %s/.skifity/Dockerfile <<'SKIFITY_DOCKERFILE'
%sSKIFITY_DOCKERFILE
`, workspace, workspace, dockerfile)
}

// cacheRef is where the build cache lives: the image's own repository, at a
// fixed tag next to the tags that hold the images themselves.
func cacheRef(image string) string {
	if idx := strings.LastIndex(image, ":"); idx > strings.LastIndex(image, "/") {
		return image[:idx] + ":buildcache"
	}
	return image + ":buildcache"
}

func buildResources(s JobSpec) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", s.CPURequestM)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", s.MemRequestMB)),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", s.MemLimitMB)),
		},
	}
}

// ImageName builds the image reference for a deployment.
func ImageName(registry, namespace, appSlug, tag string) string {
	registry = strings.TrimSuffix(registry, "/")
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s/%s/%s:%s", registry, namespace, appSlug, tag)
}

// flag renders one buildctl option with its value quoted, so a value
// containing a space or a shell metacharacter cannot change the command.
func flag(name, value string) string {
	return fmt.Sprintf("%s %q", name, value)
}

func sortedPairs(m map[string]string) [][2]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]string{k, m[k]})
	}
	return out
}

func isHex(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func ptr[T any](v T) *T { return &v }
