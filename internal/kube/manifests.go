package kube

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"skifity/internal/version"
)

// This file renders an AppSpec into Kubernetes objects. The defaults encoded
// here are the "safe and correct defaults" the product promises: health checks,
// resource limits, a non-root container, a read-only root filesystem where it is
// possible, HTTPS, and instances spread across servers.

// BuildDeployment renders the Deployment for an app.
func BuildDeployment(s AppSpec) *appsv1.Deployment {
	replicas := s.DesiredReplicas()
	labels := s.Labels()

	container := corev1.Container{
		Name:            s.Name,
		Image:           s.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         s.Command,
		Args:            s.Args,
		Resources:       buildResources(s),
		SecurityContext: &corev1.SecurityContext{
			// A container that cannot gain privileges and does not run as root
			// limits what a compromised app can do to the node it shares.
			AllowPrivilegeEscalation: ptr(false),
			RunAsNonRoot:             ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}

	if s.Port > 0 {
		container.Ports = []corev1.ContainerPort{{
			Name:          "http",
			ContainerPort: int32(s.Port),
			Protocol:      corev1.ProtocolTCP,
		}}
		container.ReadinessProbe = buildProbe(s, 3, 2)
		container.LivenessProbe = buildProbe(s, 10, 6)
		// A startup probe gives a slow framework time to boot without making
		// the liveness probe lenient forever afterwards.
		container.StartupProbe = &corev1.Probe{
			ProbeHandler:     probeHandler(s),
			PeriodSeconds:    3,
			FailureThreshold: 40, // up to two minutes to start
			TimeoutSeconds:   3,
		}
	}

	if s.EnvFromSecret != "" {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: s.EnvFromSecret},
			},
		}}
	}
	container.Env = buildPlainEnv(s)

	for _, v := range s.Volumes {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      v.Name,
			MountPath: v.MountPath,
		})
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr(true),
			// 1000 is the conventional first non-root user, and is what the
			// builders produce images for.
			RunAsUser:  ptr(int64(1000)),
			RunAsGroup: ptr(int64(1000)),
			FSGroup:    ptr(int64(1000)),
		},
		// An app has no business talking to the Kubernetes API, and a mounted
		// token is the first thing an attacker looks for.
		AutomountServiceAccountToken: ptr(false),
		// Long enough for a web server to finish in-flight requests, short
		// enough that a deploy does not feel stuck.
		TerminationGracePeriodSeconds: ptr(int64(30)),
	}

	for _, v := range s.Volumes {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: v.Name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: ResourceName(s.Name, v.Name),
				},
			},
		})
	}

	if s.SpreadAcrossServers {
		// ScheduleAnyway rather than DoNotSchedule: on a one-node cluster,
		// which is where most installs start, DoNotSchedule would leave every
		// instance after the first stuck in Pending.
		podSpec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{{
			MaxSkew:           1,
			TopologyKey:       "kubernetes.io/hostname",
			WhenUnsatisfiable: corev1.ScheduleAnyway,
			LabelSelector:     &metav1.LabelSelector{MatchLabels: s.SelectorLabels()},
		}}
		podSpec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						TopologyKey:   "kubernetes.io/hostname",
						LabelSelector: &metav1.LabelSelector{MatchLabels: s.SelectorLabels()},
					},
				}},
			},
		}
	}

	// A volume means the app keeps state on disk, so two instances writing at
	// once would corrupt it. Recreate stops the old instance before the new one
	// starts; RollingUpdate would run both.
	strategy := appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: intOrString(0),
			MaxSurge:       intOrString(1),
		},
	}
	if len(s.Volumes) > 0 {
		strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        s.Name,
			Namespace:   s.Namespace,
			Labels:      labels,
			Annotations: s.Annotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: s.SelectorLabels()},
			Strategy: strategy,
			// Ten revisions is enough history for rollback without filling
			// etcd with ReplicaSets nobody will ever use.
			RevisionHistoryLimit: ptr(int32(10)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: s.Annotations(),
				},
				Spec: podSpec,
			},
		},
	}
}

// BuildService renders the Service that sits in front of an app's instances.
func BuildService(s AppSpec) *corev1.Service {
	if s.Port <= 0 {
		return nil
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Name,
			Namespace: s.Namespace,
			Labels:    s.Labels(),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: s.SelectorLabels(),
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       80,
				TargetPort: intstr.FromString("http"),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

// BuildIngress renders the Ingress for an app's domains.
//
// Returns nil when the app has no domains, which is normal for a worker.
func BuildIngress(s AppSpec) *networkingv1.Ingress {
	if s.Port <= 0 || len(s.Domains) == 0 {
		return nil
	}

	annotations := map[string]string{}
	var tlsHosts []string
	for _, d := range s.Domains {
		if d.TLS {
			tlsHosts = append(tlsHosts, d.Hostname)
		}
	}
	if len(tlsHosts) > 0 && s.ClusterIssuer != "" {
		annotations["cert-manager.io/cluster-issuer"] = s.ClusterIssuer
		// Traefik's redirect middleware is namespaced, and the one Skifity
		// installs lives in the system namespace.
		annotations["traefik.ingress.kubernetes.io/router.middlewares"] = "skifity-system-redirect-https@kubernetescrd"
	}

	// An app that can scale to zero is reached through KEDA's interceptor,
	// which is what wakes it. Pointing the Ingress straight at the app's own
	// Service would mean a request to a sleeping app got a 503 and nothing
	// ever started it.
	backend := s.Name
	if ScaleToZeroEnabled(s) {
		backend = InterceptorServiceName(s.Name)
	}

	pathType := networkingv1.PathTypePrefix
	rules := make([]networkingv1.IngressRule, 0, len(s.Domains))
	for _, d := range s.Domains {
		path := d.Path
		if path == "" {
			path = "/"
		}
		rules = append(rules, networkingv1.IngressRule{
			Host: d.Hostname,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path:     path,
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: backend,
								Port: networkingv1.ServiceBackendPort{Number: 80},
							},
						},
					}},
				},
			},
		})
	}

	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        s.Name,
			Namespace:   s.Namespace,
			Labels:      s.Labels(),
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{Rules: rules},
	}
	if len(tlsHosts) > 0 {
		ingress.Spec.TLS = []networkingv1.IngressTLS{{
			Hosts:      tlsHosts,
			SecretName: ResourceName(s.Name, "tls"),
		}}
	}
	return ingress
}

// BuildHPA renders the HorizontalPodAutoscaler, or nil when autoscaling is off.
func BuildHPA(s AppSpec) *autoscalingv2.HorizontalPodAutoscaler {
	if !s.Autoscale {
		return nil
	}
	metrics := []autoscalingv2.MetricSpec{}
	if s.CPUTarget > 0 {
		metrics = append(metrics, utilizationMetric(corev1.ResourceCPU, s.CPUTarget))
	}
	if s.MemoryTarget > 0 {
		metrics = append(metrics, utilizationMetric(corev1.ResourceMemory, s.MemoryTarget))
	}

	minReplicas := int32(max(s.MinReplicas, 1))
	return &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceName(s.Name, "hpa"),
			Namespace: s.Namespace,
			Labels:    s.Labels(),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1", Kind: "Deployment", Name: s.Name,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: int32(s.MaxReplicas),
			Metrics:     metrics,
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					// React to a traffic spike quickly.
					StabilizationWindowSeconds: ptr(int32(30)),
				},
				ScaleDown: &autoscalingv2.HPAScalingRules{
					// Come down slowly, so a brief dip does not drop instances
					// that are about to be needed again.
					StabilizationWindowSeconds: ptr(int32(300)),
				},
			},
		},
	}
}

// BuildPDB renders a PodDisruptionBudget so that draining a node for an upgrade
// cannot take an app's last instance with it.
//
// Returns nil for a single-instance app: a budget that cannot be satisfied would
// block node drains forever, which is worse than the brief outage.
func BuildPDB(s AppSpec) *policyv1.PodDisruptionBudget {
	if s.DesiredReplicas() < 2 && !s.Autoscale {
		return nil
	}
	minAvailable := intstr.FromInt32(1)
	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceName(s.Name, "pdb"),
			Namespace: s.Namespace,
			Labels:    s.Labels(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector:     &metav1.LabelSelector{MatchLabels: s.SelectorLabels()},
		},
	}
}

// BuildPVCs renders the persistent volume claims for an app's volumes.
func BuildPVCs(s AppSpec) []*corev1.PersistentVolumeClaim {
	out := make([]*corev1.PersistentVolumeClaim, 0, len(s.Volumes))
	for _, v := range s.Volumes {
		size := v.SizeGB
		if size < 1 {
			size = 1
		}
		claim := &corev1.PersistentVolumeClaim{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      ResourceName(s.Name, v.Name),
				Namespace: s.Namespace,
				Labels:    s.Labels(),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(strconv.Itoa(size) + "Gi"),
					},
				},
			},
		}
		if v.StorageClass != "" {
			claim.Spec.StorageClassName = &v.StorageClass
		}
		out = append(out, claim)
	}
	return out
}

// BuildEnvSecret renders the Secret holding an app's environment variables.
func BuildEnvSecret(s AppSpec, values map[string]string) *corev1.Secret {
	data := make(map[string]string, len(values))
	for k, v := range values {
		data[k] = v
	}
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceName(s.Name, "env"),
			Namespace: s.Namespace,
			Labels:    s.Labels(),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: data,
	}
}

func buildResources(s AppSpec) corev1.ResourceRequirements {
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}

	if s.CPURequestM > 0 {
		requests[corev1.ResourceCPU] = resource.MustParse(strconv.Itoa(s.CPURequestM) + "m")
	}
	if s.MemRequestMB > 0 {
		requests[corev1.ResourceMemory] = resource.MustParse(strconv.Itoa(s.MemRequestMB) + "Mi")
	}
	if s.MemLimitMB > 0 {
		limits[corev1.ResourceMemory] = resource.MustParse(strconv.Itoa(s.MemLimitMB) + "Mi")
	}
	// A CPU limit is deliberately optional. Capping CPU throttles an app that
	// briefly needs more, which shows up as mysterious latency; the memory
	// limit is what actually protects the node, because memory cannot be
	// reclaimed by slowing the app down.
	if s.CPULimitM > 0 {
		limits[corev1.ResourceCPU] = resource.MustParse(strconv.Itoa(s.CPULimitM) + "m")
	}

	out := corev1.ResourceRequirements{}
	if len(requests) > 0 {
		out.Requests = requests
	}
	if len(limits) > 0 {
		out.Limits = limits
	}
	return out
}

func probeHandler(s AppSpec) corev1.ProbeHandler {
	if s.HealthPath != "" {
		return corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: s.HealthPath,
				Port: intstr.FromString("http"),
			},
		}
	}
	// Without a health path, a TCP connect is the only check that does not risk
	// calling an endpoint with side effects.
	return corev1.ProbeHandler{
		TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("http")},
	}
}

func buildProbe(s AppSpec, period, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:     probeHandler(s),
		PeriodSeconds:    period,
		TimeoutSeconds:   3,
		FailureThreshold: failureThreshold,
	}
}

func buildPlainEnv(s AppSpec) []corev1.EnvVar {
	env := []corev1.EnvVar{}
	if s.Port > 0 {
		// Most frameworks read PORT; setting it means an app usually works
		// without the user configuring anything.
		env = append(env, corev1.EnvVar{Name: "PORT", Value: strconv.Itoa(s.Port)})
	}
	// The downward API gives an app its own identity without a service account.
	env = append(env,
		corev1.EnvVar{Name: version.Name + "_APP", Value: s.Name},
		corev1.EnvVar{Name: version.Name + "_ENVIRONMENT", Value: s.Environment},
		corev1.EnvVar{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}},
	)
	for k, v := range sortedPairs(s.PlainEnv) {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}
	return env
}

func utilizationMetric(name corev1.ResourceName, target int) autoscalingv2.MetricSpec {
	value := int32(target)
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: name,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &value,
			},
		},
	}
}

// sortedPairs iterates a map in key order, so generated manifests are stable and
// a diff between two deploys shows real changes only.
func sortedPairs(m map[string]string) func(func(string, string) bool) {
	return func(yield func(string, string) bool) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func ptr[T any](v T) *T { return &v }

func intOrString(v int32) *intstr.IntOrString {
	out := intstr.FromInt32(v)
	return &out
}

// ObjectSummary is a short description of a rendered object, for logs and the
// Advanced view's list.
func ObjectSummary(kind, name string) string { return fmt.Sprintf("%s/%s", kind, name) }
