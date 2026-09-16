package dbsvc

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func postgresSpec() Spec {
	s := Spec{
		Name: "main", Namespace: "acme-shop-production", DatabaseID: "db_1", TeamID: "team_1",
		Engine: EnginePostgres, Instances: 1, StorageGB: 10,
		Username: "app", Password: "s3cret", DatabaseName: "app",
	}
	s.Defaults()
	return s
}

func TestConnectionURLs(t *testing.T) {
	cases := []struct {
		engine string
		prefix string
		host   string
	}{
		// PostgreSQL must point at the primary: writing to a replica fails, and
		// CloudNativePG exposes the primary as <cluster>-rw.
		{EnginePostgres, "postgresql://app:s3cret@main-rw.acme-shop-production.svc.cluster.local:5432/app", "main-rw"},
		{EngineRedis, "redis://default:s3cret@main.acme-shop-production.svc.cluster.local:6379", "main"},
		{EngineMySQL, "mysql://app:s3cret@main.acme-shop-production.svc.cluster.local:3306/app", "main"},
	}
	for _, tc := range cases {
		s := postgresSpec()
		s.Engine = tc.engine
		s.Defaults()
		url := s.ConnectionURL()
		if !strings.HasPrefix(url, tc.prefix) {
			t.Errorf("%s URL is %q, want it to start with %q", tc.engine, url, tc.prefix)
		}
		if !strings.Contains(s.ServiceHost(), tc.host+".") {
			t.Errorf("%s host is %q, want %q", tc.engine, s.ServiceHost(), tc.host)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := postgresSpec().Validate(); err != nil {
		t.Fatalf("a valid spec was rejected: %v", err)
	}
	cases := map[string]func(*Spec){
		"bad name":       func(s *Spec) { s.Name = "Not Valid" },
		"unknown engine": func(s *Spec) { s.Engine = "mongo" },
		"no password":    func(s *Spec) { s.Password = "" },
		"even instances": func(s *Spec) { s.Instances = 2 },
	}
	for name, mutate := range cases {
		s := postgresSpec()
		mutate(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: an invalid spec was accepted", name)
		}
	}
	// Three is fine: a failover keeps a majority.
	s := postgresSpec()
	s.Instances = 3
	if err := s.Validate(); err != nil {
		t.Fatalf("three instances were rejected: %v", err)
	}
}

func TestPostgresManifest(t *testing.T) {
	object := BuildPostgres(postgresSpec())
	if object.GetKind() != "Cluster" || object.GetAPIVersion() != "postgresql.cnpg.io/v1" {
		t.Fatalf("rendered %s/%s", object.GetAPIVersion(), object.GetKind())
	}

	instances, found, _ := unstructured.NestedInt64(object.Object, "spec", "instances")
	if !found || instances != 1 {
		t.Fatalf("instances is %d", instances)
	}
	size, _, _ := unstructured.NestedString(object.Object, "spec", "storage", "size")
	if size != "10Gi" {
		t.Fatalf("storage is %q, want 10Gi", size)
	}
	// Bootstrapping from the panel's own Secret is what ties the stored
	// credentials to the ones the database actually uses.
	secret, _, _ := unstructured.NestedString(object.Object, "spec", "bootstrap", "initdb", "secret", "name")
	if secret != "main-credentials" {
		t.Fatalf("bootstrap secret is %q", secret)
	}
	// The superuser is disabled: an app has no reason to be able to drop the
	// cluster.
	superuser, found, _ := unstructured.NestedBool(object.Object, "spec", "enableSuperuserAccess")
	if !found || superuser {
		t.Fatal("superuser access is enabled")
	}
	// PostgreSQL's defaults assume a dedicated machine; in a container they
	// have to be sized to the memory limit.
	buffers, _, _ := unstructured.NestedString(object.Object, "spec", "postgresql", "parameters", "shared_buffers")
	if buffers == "" {
		t.Fatal("shared_buffers was not sized for the container")
	}
}

func TestRedisManifest(t *testing.T) {
	s := postgresSpec()
	s.Engine = EngineRedis
	s.Defaults()

	objects := BuildRedis(s)
	if len(objects) != 2 {
		t.Fatalf("got %d objects, want a StatefulSet and a Service", len(objects))
	}
	statefulSet, ok := objects[0].(*appsv1.StatefulSet)
	if !ok {
		t.Fatalf("the first object is %T", objects[0])
	}

	container := statefulSet.Spec.Template.Spec.Containers[0]
	// The password must reach Redis through the environment, not the command
	// line, where it would be visible to anything that can read /proc.
	command := strings.Join(container.Command, " ")
	if strings.Contains(command, "s3cret") {
		t.Fatalf("the password is on the command line: %s", command)
	}
	if !strings.Contains(command, "$REDIS_PASSWORD") {
		t.Fatalf("the password is not read from the environment: %s", command)
	}
	if len(container.Env) == 0 || container.Env[0].ValueFrom == nil {
		t.Fatal("the password does not come from a Secret")
	}
	// Persistence: without appendonly, a restart loses everything.
	if !strings.Contains(command, "--appendonly yes") {
		t.Fatal("Redis is not persisting to disk, so a restart would lose the data")
	}
	if len(statefulSet.Spec.VolumeClaimTemplates) != 1 {
		t.Fatal("no volume was requested")
	}
	if container.SecurityContext == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("the container can gain privileges")
	}
}

func TestMySQLManifest(t *testing.T) {
	s := postgresSpec()
	s.Engine = EngineMySQL
	s.Defaults()

	objects := BuildMySQL(s)
	statefulSet := objects[0].(*appsv1.StatefulSet)
	container := statefulSet.Spec.Template.Spec.Containers[0]

	// Every credential must come from the Secret.
	for _, env := range container.Env {
		if env.Value != "" {
			t.Errorf("%s is set to a literal value in the manifest", env.Name)
		}
		if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
			t.Errorf("%s does not come from a Secret", env.Name)
		}
	}
	if container.ReadinessProbe == nil {
		t.Fatal("there is no readiness probe, so an app would connect before the database is up")
	}
}

func TestSecretCarriesEverythingAnAppNeeds(t *testing.T) {
	secret := BuildSecret(postgresSpec())
	if secret.Name != "main-credentials" {
		t.Fatalf("secret name is %q", secret.Name)
	}
	for _, key := range []string{"username", "password", "database", "host", "port", "url"} {
		if secret.StringData[key] == "" {
			t.Errorf("the secret has no %s", key)
		}
	}
	if secret.Type != corev1.SecretTypeOpaque {
		t.Fatalf("secret type is %q", secret.Type)
	}
}

func TestSelectorIsStable(t *testing.T) {
	// A StatefulSet's selector is immutable. Anything in it that could change
	// would make the next update fail with an error nobody can act on.
	labels := postgresSpec().Labels()
	selected := selector(labels)
	for key := range selected {
		switch key {
		case "app.kubernetes.io/name", "app.kubernetes.io/component":
		default:
			t.Errorf("the selector contains %q, which may change over a database's life", key)
		}
	}
}

func TestDefaultsAreSensible(t *testing.T) {
	s := Spec{Name: "main", Engine: EnginePostgres, Password: "x"}
	s.Defaults()
	if s.Version == "" {
		t.Fatal("no default version")
	}
	if s.StorageGB < 1 || s.Instances < 1 {
		t.Fatalf("defaults left %d GB and %d instances", s.StorageGB, s.Instances)
	}
	if s.MemLimitMB <= s.MemRequestMB {
		t.Fatalf("the memory limit (%d) is not above the reservation (%d)", s.MemLimitMB, s.MemRequestMB)
	}
}

func TestInterpretCNPGStatus(t *testing.T) {
	cases := []struct {
		phase string
		ready int64
		want  string
	}{
		{"Cluster in healthy state", 1, "running"},
		{"Setting up primary", 0, "starting"},
		{"Failed to create cluster", 0, "failed"},
		{"", 0, "starting"},
	}
	for _, tc := range cases {
		object := &unstructured.Unstructured{Object: map[string]any{
			"status": map[string]any{"phase": tc.phase, "readyInstances": tc.ready},
		}}
		if got := interpretCNPGStatus(object); got != tc.want {
			t.Errorf("phase %q with %d ready = %q, want %q", tc.phase, tc.ready, got, tc.want)
		}
	}
}
