package backup

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"skifity/internal/dbsvc"
	"skifity/internal/kube"
	"skifity/internal/version"
)

// JobSpec describes a backup or restore job.
type JobSpec struct {
	Name      string
	Namespace string
	// Engine decides which dump tool runs.
	Engine string
	// CredentialsSecret is the database's own Secret, which the job reads the
	// host, user and password from.
	CredentialsSecret string
	// URL is the presigned upload or download URL. It is passed through a
	// Secret rather than an argument, because a presigned URL carries a
	// signature that grants access to the bucket for its lifetime.
	URLSecret string
	// Restore inverts the direction.
	Restore bool
	// BackupID ties the job back to the panel's record.
	BackupID string
	Image    string
	// TimeoutSeconds bounds the job.
	TimeoutSeconds int
}

// Defaults fills in the image and timeout.
func (s *JobSpec) Defaults() {
	if s.Image == "" {
		switch s.Engine {
		case dbsvc.EnginePostgres:
			// The client version must be at least the server's, so a recent
			// image is used rather than one pinned to the server version.
			s.Image = "postgres:17-alpine"
		case dbsvc.EngineMySQL:
			s.Image = "mariadb:11.4"
		case dbsvc.EngineRedis:
			s.Image = "redis:7-alpine"
		default:
			s.Image = "alpine:3"
		}
	}
	if s.TimeoutSeconds == 0 {
		s.TimeoutSeconds = 2 * 60 * 60
	}
}

// Validate reports a job that could not work.
func (s JobSpec) Validate() error {
	if s.Name == "" || s.Namespace == "" {
		return fmt.Errorf("a backup job needs a name and a namespace")
	}
	if s.CredentialsSecret == "" {
		return fmt.Errorf("a backup job needs the database's credentials")
	}
	if s.URLSecret == "" {
		return fmt.Errorf("a backup job needs somewhere to read or write the backup")
	}
	switch s.Engine {
	case dbsvc.EnginePostgres, dbsvc.EngineMySQL, dbsvc.EngineRedis:
		return nil
	default:
		return fmt.Errorf("%q is not an engine Skifity can back up", s.Engine)
	}
}

// BuildJob renders the Kubernetes Job that does the work.
func BuildJob(s JobSpec) (*batchv1.Job, error) {
	s.Defaults()
	if err := s.Validate(); err != nil {
		return nil, err
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       s.Name,
		"app.kubernetes.io/managed-by": version.Binary,
		"app.kubernetes.io/component":  "backup",
		version.LabelKey("backup-id"):  s.BackupID,
	}

	script := backupScript(s)
	if s.Restore {
		script = restoreScript(s)
	}

	backoff := int32(0)
	deadline := int64(s.TimeoutSeconds)
	ttl := int32(3600)

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name: s.Name, Namespace: s.Namespace, Labels: labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			ActiveDeadlineSeconds:   &deadline,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: ptr(false),
					Containers: []corev1.Container{{
						Name:    "backup",
						Image:   s.Image,
						Command: []string{"/bin/sh", "-c"},
						Args:    []string{script},
						Env: []corev1.EnvVar{
							secretEnv("DB_HOST", s.CredentialsSecret, "host"),
							secretEnv("DB_PORT", s.CredentialsSecret, "port"),
							secretEnv("DB_USER", s.CredentialsSecret, "username"),
							secretEnv("DB_PASSWORD", s.CredentialsSecret, "password"),
							secretEnv("DB_NAME", s.CredentialsSecret, "database"),
							// The presigned URL is a credential in its own
							// right: it grants access to the bucket until it
							// expires, so it is never an argument.
							secretEnv("BACKUP_URL", s.URLSecret, "url"),
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
						},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}, nil
}

// backupScript dumps a database and uploads it.
//
// The pipeline never touches disk: a dump large enough to matter is also large
// enough to fill the container's writable layer.
func backupScript(s JobSpec) string {
	var dump string
	switch s.Engine {
	case dbsvc.EnginePostgres:
		// --clean --if-exists makes the dump restorable over an existing
		// database, which is what a restore actually does.
		dump = `PGPASSWORD="$DB_PASSWORD" pg_dump --host="$DB_HOST" --port="$DB_PORT" ` +
			`--username="$DB_USER" --dbname="$DB_NAME" --no-owner --no-privileges --clean --if-exists`
	case dbsvc.EngineMySQL:
		dump = `mariadb-dump --host="$DB_HOST" --port="$DB_PORT" --user="$DB_USER" ` +
			`--password="$DB_PASSWORD" --single-transaction --quick --routines --events "$DB_NAME"`
	case dbsvc.EngineRedis:
		// --rdb writes a point-in-time snapshot without stopping the server.
		dump = `redis-cli -h "$DB_HOST" -p "$DB_PORT" -a "$DB_PASSWORD" --no-auth-warning --rdb /dev/stdout`
	}

	return fmt.Sprintf(`set -eu
set -o pipefail 2>/dev/null || true

echo "==> Backing up $DB_NAME"

# apk is only needed for curl and gzip on the slim database images.
if ! command -v curl >/dev/null 2>&1; then
  apk add --no-cache curl gzip >/dev/null 2>&1 || apt-get update -qq && apt-get install -y -qq curl gzip
fi

# The dump is streamed straight to the storage service: writing it to disk
# first would need as much free space as the database.
%s | gzip -c | curl --fail --silent --show-error --upload-file - "$BACKUP_URL"

echo "==> Backup uploaded"
`, dump)
}

// restoreScript downloads a backup and loads it.
func restoreScript(s JobSpec) string {
	var load string
	switch s.Engine {
	case dbsvc.EnginePostgres:
		load = `PGPASSWORD="$DB_PASSWORD" psql --host="$DB_HOST" --port="$DB_PORT" ` +
			`--username="$DB_USER" --dbname="$DB_NAME" --quiet --set ON_ERROR_STOP=on`
	case dbsvc.EngineMySQL:
		load = `mariadb --host="$DB_HOST" --port="$DB_PORT" --user="$DB_USER" ` +
			`--password="$DB_PASSWORD" "$DB_NAME"`
	case dbsvc.EngineRedis:
		// Redis cannot load an RDB over a running server, so the restore
		// replays the keys instead. This is slower but does not need the pod
		// to be stopped and the volume swapped.
		load = `redis-cli -h "$DB_HOST" -p "$DB_PORT" -a "$DB_PASSWORD" --no-auth-warning --pipe`
	}

	return fmt.Sprintf(`set -eu
set -o pipefail 2>/dev/null || true

echo "==> Restoring $DB_NAME"

if ! command -v curl >/dev/null 2>&1; then
  apk add --no-cache curl gzip >/dev/null 2>&1 || apt-get update -qq && apt-get install -y -qq curl gzip
fi

curl --fail --silent --show-error --location "$BACKUP_URL" | gzip -dc | %s

echo "==> Restore finished"
`, load)
}

// URLSecret renders the Secret carrying a presigned URL.
func URLSecret(name, namespace, presigned string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Labels: map[string]string{"app.kubernetes.io/managed-by": version.Binary},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{"url": presigned},
	}
}

// JobName builds a unique, valid name for a backup job.
func JobName(prefix, backupID string) string {
	id := backupID
	if idx := strings.IndexByte(id, '_'); idx >= 0 {
		id = id[idx+1:]
	}
	if len(id) > 10 {
		id = id[:10]
	}
	return kube.ResourceName(prefix, id)
}

func secretEnv(name, secret, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secret},
				Key:                  key,
			},
		},
	}
}

func ptr[T any](v T) *T { return &v }
