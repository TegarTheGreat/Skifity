package kube

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"skifity/internal/version"
)

func baseSpec() AppSpec {
	return AppSpec{
		Name:         "web",
		Namespace:    "acme-shop-production",
		AppID:        "app_123",
		ProjectID:    "prj_123",
		TeamID:       "team_123",
		Environment:  "production",
		DisplayName:  "Web",
		Image:        "registry.internal/acme/web:abc123",
		Port:         3000,
		HealthPath:   "/healthz",
		Replicas:     1,
		CPURequestM:  50,
		CPULimitM:    1000,
		MemRequestMB: 128,
		MemLimitMB:   512,
	}
}

func TestSpecValidate(t *testing.T) {
	if err := baseSpec().Validate(); err != nil {
		t.Fatalf("a valid spec was rejected: %v", err)
	}

	cases := map[string]func(*AppSpec){
		"bad name":            func(s *AppSpec) { s.Name = "Not A Name" },
		"bad namespace":       func(s *AppSpec) { s.Namespace = "UPPER" },
		"no image":            func(s *AppSpec) { s.Image = "" },
		"port out of range":   func(s *AppSpec) { s.Port = 70000 },
		"cpu limit below req": func(s *AppSpec) { s.CPULimitM = 10 },
		"mem limit below req": func(s *AppSpec) { s.MemLimitMB = 10 },
		"autoscale no target": func(s *AppSpec) { s.Autoscale = true; s.MinReplicas = 1; s.MaxReplicas = 3 },
		"autoscale bad range": func(s *AppSpec) {
			s.Autoscale, s.MinReplicas, s.MaxReplicas, s.CPUTarget = true, 5, 2, 70
		},
		"relative mount": func(s *AppSpec) {
			s.Volumes = []VolumeSpec{{Name: "data", MountPath: "data", SizeGB: 1}}
		},
		"duplicate mount": func(s *AppSpec) {
			s.Volumes = []VolumeSpec{
				{Name: "a", MountPath: "/data", SizeGB: 1},
				{Name: "b", MountPath: "/data", SizeGB: 1},
			}
		},
		"bad hostname": func(s *AppSpec) {
			s.Domains = []DomainSpec{{Hostname: "not a host", TLS: true}}
		},
		"duplicate hostname": func(s *AppSpec) {
			s.Domains = []DomainSpec{
				{Hostname: "a.example.com", TLS: true},
				{Hostname: "a.example.com", TLS: true},
			}
		},
	}
	for name, mutate := range cases {
		s := baseSpec()
		mutate(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: Validate accepted an invalid spec", name)
		}
	}
}

func TestDeploymentDefaultsAreSafe(t *testing.T) {
	d := BuildDeployment(baseSpec())

	if *d.Spec.Replicas != 1 {
		t.Fatalf("replicas = %d, want 1", *d.Spec.Replicas)
	}
	container := d.Spec.Template.Spec.Containers[0]

	// Health checks: an app without them is restarted blindly and routed to
	// before it is ready.
	if container.ReadinessProbe == nil || container.LivenessProbe == nil || container.StartupProbe == nil {
		t.Fatal("a container was rendered without all three probes")
	}
	if container.ReadinessProbe.HTTPGet == nil || container.ReadinessProbe.HTTPGet.Path != "/healthz" {
		t.Fatalf("readiness probe does not use the configured health path: %+v", container.ReadinessProbe)
	}

	// Resource limits: without a memory limit one app can take down a node.
	if container.Resources.Limits.Memory().IsZero() {
		t.Fatal("no memory limit was set")
	}
	if container.Resources.Requests.Cpu().IsZero() {
		t.Fatal("no CPU reservation was set")
	}

	// Security context.
	sc := container.SecurityContext
	if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Fatal("privilege escalation is not blocked")
	}
	if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Fatal("the container is allowed to run as root")
	}
	if len(sc.Capabilities.Drop) == 0 || sc.Capabilities.Drop[0] != "ALL" {
		t.Fatal("capabilities are not dropped")
	}
	if d.Spec.Template.Spec.AutomountServiceAccountToken == nil ||
		*d.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("the Kubernetes API token is mounted into the app, which it never needs")
	}

	// PORT is what almost every framework reads.
	var sawPort bool
	for _, e := range container.Env {
		if e.Name == "PORT" && e.Value == "3000" {
			sawPort = true
		}
	}
	if !sawPort {
		t.Fatal("PORT was not set from the configured port")
	}
}

func TestDeploymentSelectorIsStableAcrossDeploys(t *testing.T) {
	// A Deployment's selector is immutable. If anything that changes per deploy
	// leaks into it, every second deploy fails with an error nobody can act on.
	first := baseSpec()
	first.DeploymentID = "dep_1"
	first.Revision = "rev-1"
	second := baseSpec()
	second.DeploymentID = "dep_2"
	second.Revision = "rev-2"

	a := BuildDeployment(first).Spec.Selector.MatchLabels
	b := BuildDeployment(second).Spec.Selector.MatchLabels
	if len(a) != len(b) {
		t.Fatalf("selector changed size between deploys: %v then %v", a, b)
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("selector label %q changed between deploys: %q then %q", k, v, b[k])
		}
	}

	// The pod template annotations must differ, or the rollout is a no-op.
	pa := BuildDeployment(first).Spec.Template.Annotations
	pb := BuildDeployment(second).Spec.Template.Annotations
	if pa["skifity.io/revision"] == pb["skifity.io/revision"] {
		t.Fatal("the pod template did not change between deploys, so no rollout would happen")
	}
}

func TestVolumesForceRecreateStrategy(t *testing.T) {
	// Two instances writing the same ReadWriteOnce volume corrupts it, and a
	// rolling update would briefly run both.
	s := baseSpec()
	s.Volumes = []VolumeSpec{{Name: "data", MountPath: "/data", SizeGB: 5}}
	d := BuildDeployment(s)
	if d.Spec.Strategy.Type != "Recreate" {
		t.Fatalf("strategy is %s for an app with a volume, want Recreate", d.Spec.Strategy.Type)
	}

	claims := BuildPVCs(s)
	if len(claims) != 1 {
		t.Fatalf("got %d claims, want 1", len(claims))
	}
	if claims[0].Name != "web-data" {
		t.Fatalf("claim name %q, want web-data", claims[0].Name)
	}
	if got := claims[0].Spec.Resources.Requests[corev1.ResourceStorage]; got.String() != "5Gi" {
		t.Fatalf("claim size %s, want 5Gi", got.String())
	}
	// The pod must actually mount the claim it asks for.
	mounted := d.Spec.Template.Spec.Containers[0].VolumeMounts
	if len(mounted) != 1 || mounted[0].MountPath != "/data" {
		t.Fatalf("volume mounts are %+v", mounted)
	}
	if d.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != "web-data" {
		t.Fatal("the pod references a claim that is not the one rendered")
	}
}

func TestRollingUpdateKeepsCapacity(t *testing.T) {
	s := baseSpec()
	s.Replicas = 3
	d := BuildDeployment(s)
	if d.Spec.Strategy.Type != "RollingUpdate" {
		t.Fatalf("strategy is %s, want RollingUpdate", d.Spec.Strategy.Type)
	}
	// MaxUnavailable 0 is what makes a deploy zero-downtime.
	if d.Spec.Strategy.RollingUpdate.MaxUnavailable.IntValue() != 0 {
		t.Fatalf("maxUnavailable is %v, which allows a gap in capacity during a deploy",
			d.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}
}

func TestSpreadAcrossServersToleratesOneNode(t *testing.T) {
	s := baseSpec()
	s.Replicas = 3
	s.SpreadAcrossServers = true
	d := BuildDeployment(s)

	constraints := d.Spec.Template.Spec.TopologySpreadConstraints
	if len(constraints) != 1 {
		t.Fatalf("got %d spread constraints, want 1", len(constraints))
	}
	// DoNotSchedule would leave instances Pending forever on a one-node
	// cluster, which is where most installs start.
	if constraints[0].WhenUnsatisfiable != "ScheduleAnyway" {
		t.Fatalf("spread constraint is %s; on a single node this strands instances in Pending",
			constraints[0].WhenUnsatisfiable)
	}
	if constraints[0].TopologyKey != "kubernetes.io/hostname" {
		t.Fatalf("spreading by %q, want kubernetes.io/hostname", constraints[0].TopologyKey)
	}
}

func TestServiceTargetsNamedPort(t *testing.T) {
	svc := BuildService(baseSpec())
	if svc == nil {
		t.Fatal("no Service was rendered for an app with a port")
	}
	if svc.Spec.Ports[0].Port != 80 {
		t.Fatalf("service port is %d, want 80", svc.Spec.Ports[0].Port)
	}
	// Targeting the named port rather than the number means changing the app's
	// port does not need the Service to be edited in step.
	if svc.Spec.Ports[0].TargetPort.StrVal != "http" {
		t.Fatalf("service targets %v, want the named port http", svc.Spec.Ports[0].TargetPort)
	}
	// An app with no port is a worker and needs no Service.
	s := baseSpec()
	s.Port = 0
	if BuildService(s) != nil {
		t.Fatal("a Service was rendered for an app with no port")
	}
}

func TestIngressRendersTLSAndIssuer(t *testing.T) {
	s := baseSpec()
	s.ClusterIssuer = "letsencrypt"
	s.Domains = []DomainSpec{
		{Hostname: "shop.example.com", TLS: true},
		{Hostname: "internal.example.com", TLS: false},
	}
	ing := BuildIngress(s)
	if ing == nil {
		t.Fatal("no Ingress was rendered for an app with domains")
	}
	if len(ing.Spec.Rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(ing.Spec.Rules))
	}
	if len(ing.Spec.TLS) != 1 || len(ing.Spec.TLS[0].Hosts) != 1 {
		t.Fatalf("TLS section is %+v; only the TLS domain belongs in it", ing.Spec.TLS)
	}
	if ing.Spec.TLS[0].Hosts[0] != "shop.example.com" {
		t.Fatalf("TLS covers %q, want shop.example.com", ing.Spec.TLS[0].Hosts[0])
	}
	if ing.Annotations["cert-manager.io/cluster-issuer"] != "letsencrypt" {
		t.Fatal("the cert-manager issuer annotation is missing, so no certificate would be issued")
	}

	// No domains means no Ingress, which is correct for a worker.
	s.Domains = nil
	if BuildIngress(s) != nil {
		t.Fatal("an Ingress was rendered for an app with no domains")
	}
}

func TestIngressWithoutIssuerSkipsAnnotation(t *testing.T) {
	s := baseSpec()
	s.Domains = []DomainSpec{{Hostname: "shop.example.com", TLS: true}}
	ing := BuildIngress(s)
	if _, ok := ing.Annotations["cert-manager.io/cluster-issuer"]; ok {
		t.Fatal("an issuer annotation was set with no issuer configured, which cert-manager would reject")
	}
}

func TestHPAOnlyWhenAutoscaling(t *testing.T) {
	if BuildHPA(baseSpec()) != nil {
		t.Fatal("an HPA was rendered for an app that is not autoscaling")
	}

	s := baseSpec()
	s.Autoscale, s.MinReplicas, s.MaxReplicas, s.CPUTarget, s.MemoryTarget = true, 2, 10, 75, 80
	hpa := BuildHPA(s)
	if hpa == nil {
		t.Fatal("no HPA was rendered for an autoscaling app")
	}
	if *hpa.Spec.MinReplicas != 2 || hpa.Spec.MaxReplicas != 10 {
		t.Fatalf("HPA range is %d-%d, want 2-10", *hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas)
	}
	if len(hpa.Spec.Metrics) != 2 {
		t.Fatalf("got %d metrics, want CPU and memory", len(hpa.Spec.Metrics))
	}
	// Scaling down faster than up causes flapping under bursty traffic.
	up := *hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds
	down := *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
	if down <= up {
		t.Fatalf("scale-down window (%ds) is not longer than scale-up (%ds), which causes flapping", down, up)
	}

	// The Deployment must start at the minimum and let the HPA take over.
	d := BuildDeployment(s)
	if *d.Spec.Replicas != 2 {
		t.Fatalf("an autoscaling Deployment was rendered with %d replicas, want the minimum of 2", *d.Spec.Replicas)
	}
}

func TestPDBSkippedForSingleInstance(t *testing.T) {
	// A budget requiring one available instance on a one-instance app blocks
	// every node drain, which breaks cluster upgrades.
	if BuildPDB(baseSpec()) != nil {
		t.Fatal("a PodDisruptionBudget was rendered for a single-instance app")
	}
	s := baseSpec()
	s.Replicas = 3
	pdb := BuildPDB(s)
	if pdb == nil {
		t.Fatal("no PodDisruptionBudget for a three-instance app")
	}
	if pdb.Spec.MinAvailable.IntValue() != 1 {
		t.Fatalf("minAvailable is %v, want 1", pdb.Spec.MinAvailable)
	}
}

func TestEnvSecretCarriesValues(t *testing.T) {
	secret := BuildEnvSecret(baseSpec(), map[string]string{"DATABASE_URL": "postgres://x", "DEBUG": "false"})
	if secret.Name != "web-env" {
		t.Fatalf("secret name %q, want web-env", secret.Name)
	}
	if secret.StringData["DATABASE_URL"] != "postgres://x" {
		t.Fatal("the secret lost a value")
	}

	d := BuildDeployment(func() AppSpec { s := baseSpec(); s.EnvFromSecret = "web-env"; return s }())
	envFrom := d.Spec.Template.Spec.Containers[0].EnvFrom
	if len(envFrom) != 1 || envFrom[0].SecretRef.Name != "web-env" {
		t.Fatalf("the container does not read the env secret: %+v", envFrom)
	}
}

func TestEnvHashChangesWithValues(t *testing.T) {
	a := EnvHash(map[string]string{"A": "1", "B": "2"})
	// Key order must not matter, or every reconcile would look like a change.
	b := EnvHash(map[string]string{"B": "2", "A": "1"})
	if a != b {
		t.Fatal("EnvHash depends on map iteration order, so pods would restart for no reason")
	}
	if a == EnvHash(map[string]string{"A": "1", "B": "3"}) {
		t.Fatal("changing a value did not change the hash, so pods would not pick it up")
	}
	if a == EnvHash(map[string]string{"A": "1"}) {
		t.Fatal("removing a variable did not change the hash")
	}
}

func TestBuildFingerprintSeparatesBuildFromRuntime(t *testing.T) {
	// This is the mechanism behind ADR-0007 and the fix for the most common
	// complaint about comparable products.
	base := BuildFingerprint("https://github.com/a/b", "sha1", "railpack", "", "", nil)

	if base != BuildFingerprint("https://github.com/a/b", "sha1", "railpack", "", "", nil) {
		t.Fatal("the fingerprint is not stable for identical inputs")
	}
	if base == BuildFingerprint("https://github.com/a/b", "sha2", "railpack", "", "", nil) {
		t.Fatal("a new commit did not change the fingerprint, so it would never rebuild")
	}
	if base == BuildFingerprint("https://github.com/a/b", "sha1", "nixpacks", "", "", nil) {
		t.Fatal("changing the builder did not change the fingerprint")
	}
	if base == BuildFingerprint("https://github.com/a/b", "sha1", "railpack", "", "apps/web", nil) {
		t.Fatal("changing the root directory did not change the fingerprint")
	}
	if base == BuildFingerprint("https://github.com/a/b", "sha1", "railpack", "", "", map[string]string{"NODE_ENV": "production"}) {
		t.Fatal("a build argument did not change the fingerprint")
	}
	// Build arguments must hash in a stable order.
	args := map[string]string{"A": "1", "B": "2"}
	if BuildFingerprint("r", "c", "b", "", "", args) !=
		BuildFingerprint("r", "c", "b", "", "", map[string]string{"B": "2", "A": "1"}) {
		t.Fatal("build argument order changes the fingerprint")
	}
}

func TestLabelsCarryOwnership(t *testing.T) {
	labels := baseSpec().Labels()
	for _, key := range []string{
		"app.kubernetes.io/name", "app.kubernetes.io/managed-by",
		"skifity.io/app-id", "skifity.io/project-id", "skifity.io/team-id",
	} {
		if labels[key] == "" {
			t.Errorf("label %q is missing, so the object cannot be traced back to its owner", key)
		}
	}
	if labels["app.kubernetes.io/managed-by"] != "skifity" {
		t.Errorf("managed-by is %q", labels["app.kubernetes.io/managed-by"])
	}
}

func TestNoPortMeansNoProbes(t *testing.T) {
	// A worker has nothing to probe; a probe against no port would fail forever.
	s := baseSpec()
	s.Port = 0
	container := BuildDeployment(s).Spec.Template.Spec.Containers[0]
	if container.ReadinessProbe != nil || container.LivenessProbe != nil {
		t.Fatal("probes were rendered for an app with no port")
	}
	if len(container.Ports) != 0 {
		t.Fatal("a container port was rendered for an app with no port")
	}
}

func TestHealthPathFallsBackToTCP(t *testing.T) {
	s := baseSpec()
	s.HealthPath = ""
	container := BuildDeployment(s).Spec.Template.Spec.Containers[0]
	if container.ReadinessProbe.TCPSocket == nil {
		t.Fatal("without a health path the probe should be a TCP connect, not an HTTP request to an unknown path")
	}
}

func TestManifestsAreDeterministic(t *testing.T) {
	s := baseSpec()
	s.PlainEnv = map[string]string{"Z": "1", "A": "2", "M": "3"}
	first := BuildDeployment(s).Spec.Template.Spec.Containers[0].Env
	for range 20 {
		next := BuildDeployment(s).Spec.Template.Spec.Containers[0].Env
		if len(next) != len(first) {
			t.Fatal("environment length changed between renders")
		}
		for i := range first {
			if next[i].Name != first[i].Name {
				t.Fatalf("environment order changed between renders at %d: %q then %q",
					i, first[i].Name, next[i].Name)
			}
		}
	}
	// And the order must actually be sorted, so a diff is readable.
	var plain []string
	for _, e := range first {
		if e.Name == "Z" || e.Name == "A" || e.Name == "M" {
			plain = append(plain, e.Name)
		}
	}
	if strings.Join(plain, "") != "AMZ" {
		t.Fatalf("plain environment is ordered %v, want sorted", plain)
	}
}

func TestNamespaceIsolation(t *testing.T) {
	ns := BuildNamespace("acme-shop-production", "team_1", "prj_1")
	if ns.Labels["pod-security.kubernetes.io/enforce"] != "restricted" {
		t.Fatal("the namespace does not enforce the restricted pod security profile")
	}

	quota := BuildResourceQuota("acme-shop-production", DefaultQuota())
	// A LoadBalancer or NodePort service opens a port on every node and
	// bypasses the ingress, and with it TLS.
	if got := quota.Spec.Hard["services.loadbalancers"]; got.String() != "0" {
		t.Fatalf("LoadBalancer services are not blocked: %s", got.String())
	}
	if got := quota.Spec.Hard["services.nodeports"]; got.String() != "0" {
		t.Fatalf("NodePort services are not blocked: %s", got.String())
	}
	if mem := quota.Spec.Hard["requests.memory"]; mem.IsZero() {
		t.Fatal("no memory quota was set")
	}

	limits := BuildLimitRange("acme-shop-production")
	if limits.Spec.Limits[0].Default.Memory().IsZero() {
		t.Fatal("a container created without a memory limit would get none")
	}
}

func TestNetworkPoliciesDenyByDefaultAndBlockMetadata(t *testing.T) {
	policies := BuildNetworkPolicies("acme-shop-production", "skifity-system")
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want a deny-all and an allow", len(policies))
	}

	deny := policies[0]
	if len(deny.Spec.Ingress) != 0 || len(deny.Spec.Egress) != 0 {
		t.Fatal("the deny-all policy has rules, so it does not deny anything")
	}
	if len(deny.Spec.PolicyTypes) != 2 {
		t.Fatal("the deny-all policy does not cover both directions")
	}

	allow := policies[1]
	var sawMetadataBlock bool
	for _, rule := range allow.Spec.Egress {
		for _, peer := range rule.To {
			if peer.IPBlock == nil {
				continue
			}
			for _, except := range peer.IPBlock.Except {
				if except == "169.254.169.254/32" {
					sawMetadataBlock = true
				}
			}
		}
	}
	// The cloud metadata endpoint hands provider credentials to anything that
	// asks, which is a well-trodden path from a compromised app to the account.
	if !sawMetadataBlock {
		t.Fatal("egress to the cloud metadata endpoint is not blocked")
	}
}

// TestScaleToZeroRendersWhatItPromises: the toggle carried all the way into
// the spec and then rendered nothing at all, so turning it on installed KEDA
// and changed the app not one bit.
// scalableSpec is an app with a hostname, which is what scale to zero needs.
func scalableSpec() AppSpec {
	spec := baseSpec()
	spec.Domains = []DomainSpec{{Hostname: "shop.example.com", Path: "/", TLS: true}}
	return spec
}

func TestScaleToZeroRendersWhatItPromises(t *testing.T) {
	spec := scalableSpec()
	spec.ScaleToZero = true
	spec.Autoscale = true
	spec.MinReplicas, spec.MaxReplicas = 1, 4

	scaled := BuildHTTPScaledObject(spec)
	if scaled == nil {
		t.Fatal("scale to zero renders no KEDA object, so nothing ever scales to zero")
	}
	replicas, _, _ := unstructured.NestedMap(scaled.Object, "spec", "replicas")
	if replicas["min"] != int64(0) {
		t.Errorf("the floor is %v, want 0", replicas["min"])
	}
	if replicas["max"] != int64(4) {
		t.Errorf("the ceiling is %v, want the app's maximum", replicas["max"])
	}
	hosts, _, _ := unstructured.NestedSlice(scaled.Object, "spec", "hosts")
	if len(hosts) != len(spec.Domains) {
		t.Errorf("the object knows %d hostnames, want %d", len(hosts), len(spec.Domains))
	}

	// The interceptor is what wakes the app, so the Ingress has to go through
	// it. Pointing at the app's own Service would give a sleeping app a 503
	// and start nothing.
	ingress := BuildIngress(spec)
	if ingress == nil {
		t.Fatal("no ingress")
	}
	backend := ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name
	if backend != InterceptorServiceName(spec.Name) {
		t.Fatalf("the ingress points at %q, so a request to a sleeping app wakes nothing", backend)
	}
	if BuildInterceptorService(spec) == nil {
		t.Fatal("the ingress points at a service that is never created")
	}
}

// TestWithoutScaleToZeroNothingChanges keeps the ordinary path ordinary.
func TestWithoutScaleToZeroNothingChanges(t *testing.T) {
	spec := scalableSpec()
	spec.ScaleToZero = false

	if BuildHTTPScaledObject(spec) != nil || BuildInterceptorService(spec) != nil {
		t.Fatal("an app that does not scale to zero got KEDA objects anyway")
	}
	ingress := BuildIngress(spec)
	if got := ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name; got != spec.Name {
		t.Fatalf("the ingress points at %q, want the app's own service", got)
	}
}

// TestScaleToZeroNeedsAHostname: the interceptor routes by Host header, so an
// app nobody can reach by name has nothing to be woken by.
func TestScaleToZeroNeedsAHostname(t *testing.T) {
	spec := scalableSpec()
	spec.ScaleToZero = true
	spec.Domains = nil

	if BuildHTTPScaledObject(spec) != nil {
		t.Fatal("an app with no hostname was given a scaler that could never fire")
	}
}

// TestTheInterceptorCanReachTheApp: KEDA runs in its own namespace, and the
// environment's default-deny policy would otherwise drop the request that woke
// the app.
func TestTheInterceptorCanReachTheApp(t *testing.T) {
	policies := BuildNetworkPolicies("acme-shop-production", "skifity-system")
	var allowed bool
	for _, policy := range policies {
		for _, rule := range policy.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.NamespaceSelector == nil {
					continue
				}
				if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == KEDANamespace {
					allowed = true
				}
			}
		}
	}
	if !allowed {
		t.Fatal("an app that scales to zero wakes up and then drops the request that woke it")
	}
}

// TestARunGetsTheAppsOwnWorld is the whole point of a one-off command: a
// migration has to see the same DATABASE_URL the app sees, in the same image,
// or it migrates the wrong thing or nothing at all.
func TestARunGetsTheAppsOwnWorld(t *testing.T) {
	app := baseSpec()
	app.EnvFromSecret = "web-env"
	app.Volumes = []VolumeSpec{{Name: "data", MountPath: "/data", SizeGB: 1}}

	job, err := BuildRunJob(RunSpec{App: app, Name: "web-run-abc", Command: "npm run migrate"})
	if err != nil {
		t.Fatalf("BuildRunJob: %v", err)
	}
	pod := job.Spec.Template.Spec
	container := pod.Containers[0]

	if container.Image != app.Image {
		t.Errorf("the command runs %q, not the app's own image", container.Image)
	}
	if len(container.EnvFrom) == 0 || container.EnvFrom[0].SecretRef.Name != "web-env" {
		t.Error("the command does not get the app's variables, so a migration has no database to migrate")
	}
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != "/data" {
		t.Error("the command cannot see the app's volume")
	}
	if container.Args[0] != "npm run migrate" {
		t.Errorf("the command is %q; it should reach the shell as typed", container.Args[0])
	}
	if container.Command[0] != "/bin/sh" {
		t.Error("the command is not run through a shell, so `a && b` would not work")
	}

	// A migration that half-ran and then ran again is worse than one that
	// failed and said so.
	if *job.Spec.BackoffLimit != 0 {
		t.Errorf("backoffLimit is %d; a failed migration would be retried", *job.Spec.BackoffLimit)
	}
	if pod.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restart policy is %s, want Never", pod.RestartPolicy)
	}
	if job.Spec.ActiveDeadlineSeconds == nil {
		t.Error("a command with no deadline could run forever")
	}
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Error("the command's pod mounts an API token it does not need")
	}
}

// TestARunIsRefusedWithoutSomethingToRunIn.
func TestARunIsRefusedWithoutSomethingToRunIn(t *testing.T) {
	cases := map[string]RunSpec{
		"no image":   {App: func() AppSpec { s := baseSpec(); s.Image = ""; return s }(), Name: "x", Command: "ls"},
		"no command": {App: baseSpec(), Name: "x"},
		"no name":    {App: baseSpec(), Command: "ls"},
	}
	for name, spec := range cases {
		if _, err := BuildRunJob(spec); err == nil {
			t.Errorf("%s: an impossible run was accepted", name)
		}
	}
}

// TestReleaseAndOneOffRunsAreToldApart: they share a shape and differ in when
// they run, and an operator reading the namespace should be able to see which
// is which.
func TestReleaseAndOneOffRunsAreToldApart(t *testing.T) {
	app := baseSpec()
	release, err := BuildRunJob(RunSpec{
		App: app, Name: RunJobName(app.Name, RunKindRelease, "dep123"),
		Command: "npm run migrate", Kind: RunKindRelease,
	})
	if err != nil {
		t.Fatalf("BuildRunJob: %v", err)
	}
	oneOff, err := BuildRunJob(RunSpec{
		App: app, Name: RunJobName(app.Name, RunKindOneOff, "abc123"), Command: "ls",
	})
	if err != nil {
		t.Fatalf("BuildRunJob: %v", err)
	}

	if release.Name == oneOff.Name {
		t.Fatal("a release and a one-off command would collide on the same name")
	}
	if !strings.Contains(release.Name, "release") {
		t.Errorf("a release job is called %q", release.Name)
	}
	if release.Labels[version.LabelKey("run-kind")] != RunKindRelease {
		t.Error("a release job is not labelled as one")
	}
	for _, name := range []string{release.Name, oneOff.Name} {
		if len(name) > 63 {
			t.Errorf("%q is %d characters, which Kubernetes refuses", name, len(name))
		}
	}
}
