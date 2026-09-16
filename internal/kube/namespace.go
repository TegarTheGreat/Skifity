package kube

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"skifity/internal/version"
)

// Tenant isolation.
//
// Each environment is a namespace carrying three guards:
//
//   - a LimitRange, so a workload created without limits cannot starve a node;
//   - a ResourceQuota, so one environment cannot consume the whole cluster;
//   - a default-deny NetworkPolicy, so one team's apps cannot reach another's.
//
// The quota defaults are generous: they exist to contain a mistake, not to
// ration. An operator raises them per environment when they need to.

// NamespaceQuota describes an environment's ceiling.
type NamespaceQuota struct {
	CPUCores    int
	MemoryGB    int
	StorageGB   int
	MaxPods     int
	MaxServices int
}

// DefaultQuota is what a new environment gets.
func DefaultQuota() NamespaceQuota {
	return NamespaceQuota{CPUCores: 8, MemoryGB: 16, StorageGB: 100, MaxPods: 60, MaxServices: 30}
}

// BuildNamespace renders the Namespace object for an environment.
func BuildNamespace(name, teamID, projectID string) *corev1.Namespace {
	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": version.Binary,
				version.LabelKey("team-id"):    teamID,
				version.LabelKey("project-id"): projectID,
				// Pod Security Admission. "restricted" is the strictest
				// profile, and is what the rendered pod specs already satisfy,
				// so this catches anything that bypasses them.
				"pod-security.kubernetes.io/enforce": "restricted",
				"pod-security.kubernetes.io/audit":   "restricted",
				"pod-security.kubernetes.io/warn":    "restricted",
			},
		},
	}
}

// BuildLimitRange renders the per-container defaults for a namespace.
func BuildLimitRange(namespace string) *corev1.LimitRange {
	return &corev1.LimitRange{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "LimitRange"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": version.Binary},
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{{
				Type: corev1.LimitTypeContainer,
				// Applied when a container sets no limit of its own.
				Default: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
				DefaultRequest: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("128Mi"),
					corev1.ResourceCPU:    resource.MustParse("50m"),
				},
			}},
		},
	}
}

// BuildResourceQuota renders an environment's ceiling.
func BuildResourceQuota(namespace string, q NamespaceQuota) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ResourceQuota"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "environment",
			Namespace: namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": version.Binary},
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				"requests.cpu":     resource.MustParse(itoaResource(q.CPUCores)),
				"requests.memory":  resource.MustParse(itoaResource(q.MemoryGB) + "Gi"),
				"limits.cpu":       resource.MustParse(itoaResource(q.CPUCores * 2)),
				"limits.memory":    resource.MustParse(itoaResource(q.MemoryGB*2) + "Gi"),
				"requests.storage": resource.MustParse(itoaResource(q.StorageGB) + "Gi"),
				"pods":             resource.MustParse(itoaResource(q.MaxPods)),
				"services":         resource.MustParse(itoaResource(q.MaxServices)),
				// A LoadBalancer or NodePort service would open a port on every
				// node, bypassing the ingress and its TLS.
				"services.loadbalancers": resource.MustParse("0"),
				"services.nodeports":     resource.MustParse("0"),
			},
		},
	}
}

// BuildNetworkPolicies renders the default-deny policy and the exceptions.
//
// Two objects rather than one: a deny-all baseline and an allow policy, because
// Kubernetes network policies are additive and this is far easier to reason
// about than one combined object.
func BuildNetworkPolicies(namespace, systemNamespace string) []*networkingv1.NetworkPolicy {
	labels := map[string]string{"app.kubernetes.io/managed-by": version.Binary}
	dnsPort := intstr.FromInt32(53)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	denyAll := &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-deny", Namespace: namespace, Labels: labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			// An empty selector means every pod in the namespace.
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}

	allow := &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "allow-ingress-and-egress", Namespace: namespace, Labels: labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{
					// Traffic from the ingress controller and the panel.
					{NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
					}},
					{NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"kubernetes.io/metadata.name": systemNamespace},
					}},
					// And between pods of this same environment, so an app can
					// reach its own database.
					{PodSelector: &metav1.LabelSelector{}},
				},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					// DNS, without which nothing resolves anything.
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udp, Port: &dnsPort},
						{Protocol: &tcp, Port: &dnsPort},
					},
				},
				{
					// Within the environment.
					To: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
				},
				{
					// And out to the internet, but not to other namespaces or
					// to the node network, which is where the Kubernetes API,
					// the kubelet and cloud metadata services live.
					To: []networkingv1.NetworkPolicyPeer{{
						IPBlock: &networkingv1.IPBlock{
							CIDR: "0.0.0.0/0",
							Except: []string{
								"10.0.0.0/8",
								"172.16.0.0/12",
								"192.168.0.0/16",
								// The cloud metadata endpoint, which hands out
								// provider credentials to anything that asks.
								"169.254.169.254/32",
							},
						},
					}},
				},
			},
		},
	}
	return []*networkingv1.NetworkPolicy{denyAll, allow}
}

func itoaResource(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
