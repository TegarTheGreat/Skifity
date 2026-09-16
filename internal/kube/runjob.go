package kube

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"skifity/internal/version"
)

// One-off commands.
//
// Every product like this needs a way to run something once, in the app's own
// image, with the app's own variables: a database migration, a management
// command, a look around. Without it a migration has nowhere to run, and the
// answer people reach for is a shell on the server and kubectl — which is the
// thing the panel exists to avoid.
//
// A run is a Kubernetes Job, not an exec into a running pod. An exec needs a
// pod that is already up, which is exactly what a failed migration prevents,
// and it competes for the CPU and memory the app is serving with. A Job runs
// on its own, can be watched, and leaves a log.

// RunKindOneOff is a command somebody asked for.
const RunKindOneOff = "one-off"

// RunKindRelease is the release command, run as part of a deployment.
const RunKindRelease = "release"

// RunSpec is a one-off command in an app's own environment.
type RunSpec struct {
	// App is the app the command borrows its image, variables, namespace and
	// resource limits from.
	App AppSpec
	// Name is the Job's name, which is also how its log is found again.
	Name string
	// Command is a shell line, run the way the app's start command is: people
	// type `npm run migrate && npm run seed`, and splitting that here would
	// get the quoting subtly wrong.
	Command string
	// Kind is one-off or release, and is a label so the two can be told apart.
	Kind string
	// TimeoutSeconds bounds the run. Zero means thirty minutes.
	TimeoutSeconds int
}

// Validate reports a run that could not work.
func (s RunSpec) Validate() error {
	if s.Name == "" || s.App.Namespace == "" {
		return fmt.Errorf("a run needs a name and a namespace")
	}
	if s.App.Image == "" {
		return fmt.Errorf("this app has not been built yet, so there is no image to run the command in")
	}
	if s.Command == "" {
		return fmt.Errorf("a run needs a command")
	}
	return nil
}

// BuildRunJob renders the Job for a one-off command.
func BuildRunJob(s RunSpec) (*batchv1.Job, error) {
	if s.TimeoutSeconds == 0 {
		s.TimeoutSeconds = 30 * 60
	}
	if s.Kind == "" {
		s.Kind = RunKindOneOff
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}

	labels := s.App.Labels()
	labels["app.kubernetes.io/component"] = "run"
	labels[version.LabelKey("run-kind")] = s.Kind

	container := corev1.Container{
		Name:            "run",
		Image:           s.App.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		// A shell, because that is what the person typed into.
		Command:   []string{"/bin/sh", "-c"},
		Args:      []string{s.Command},
		Resources: buildResources(s.App),
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr(false),
			RunAsNonRoot:             ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}
	if s.App.EnvFromSecret != "" {
		// The whole point: the same DATABASE_URL the app runs with.
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: s.App.EnvFromSecret},
			},
		}}
	}
	container.Env = buildPlainEnv(s.App)

	// Volumes are mounted too, because a command that writes a file the app
	// then reads has to write it where the app looks.
	for _, v := range s.App.Volumes {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name: v.Name, MountPath: v.MountPath,
		})
	}

	podSpec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Containers:    []corev1.Container{container},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr(true),
			RunAsUser:    ptr(int64(1000)),
			RunAsGroup:   ptr(int64(1000)),
			FSGroup:      ptr(int64(1000)),
		},
		AutomountServiceAccountToken: ptr(false),
	}
	for _, v := range s.App.Volumes {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: v.Name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: ResourceName(s.App.Name, v.Name),
				},
			},
		})
	}

	// No retries. A migration that half-ran and then ran again is worse than
	// one that failed and said so.
	backoff := int32(0)
	deadline := int64(s.TimeoutSeconds)
	// Long enough that somebody can read the log after it finished, short
	// enough that finished Jobs do not pile up in the namespace.
	ttl := int32(3600)

	return &batchv1.Job{
		TypeMeta:   metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{Name: s.Name, Namespace: s.App.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			ActiveDeadlineSeconds:   &deadline,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}, nil
}

// RunJobName builds a unique, valid name for a run.
func RunJobName(appSlug, kind, id string) string {
	prefix := appSlug + "-run"
	if kind == RunKindRelease {
		prefix = appSlug + "-release"
	}
	if len(id) > 10 {
		id = id[:10]
	}
	return ResourceName(prefix, id)
}
