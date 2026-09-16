package kube

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// These tests run against client-go's fake clientset, so the same code path that
// talks to a real API server is exercised: the requests are built, sent and
// decoded for real, only the transport is replaced.

// toObjects converts a heterogeneous list into the runtime.Object slice the
// fake clientset takes.
func toObjects(items []any) []runtime.Object {
	out := make([]runtime.Object, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(runtime.Object); ok {
			out = append(out, obj)
		}
	}
	return out
}

// unstructuredString reads a nested string from a parsed document.
func unstructuredString(u *unstructured.Unstructured, fields ...string) (string, bool, error) {
	return unstructured.NestedString(u.Object, fields...)
}

func TestAppStatusExplainsCrashLoop(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "acme-shop-production", Generation: 1},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "web"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "img:1"}}},
			},
		},
		Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 0, ObservedGeneration: 1},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-abc", Namespace: "acme-shop-production",
			Labels: map[string]string{"app.kubernetes.io/name": "web"},
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				RestartCount: 7,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason:  "CrashLoopBackOff",
					Message: "back-off 5m0s restarting failed container",
				}},
			}},
		},
	}

	c := &Client{clientset: fake.NewSimpleClientset(deployment, pod), systemNamespace: "skifity-system"}
	status, err := c.AppStatus(t.Context(), "acme-shop-production", "web")
	if err != nil {
		t.Fatalf("AppStatus: %v", err)
	}
	// The phase must say what is wrong in plain language, not "0/1 ready".
	if status.Phase != "crashing" {
		t.Fatalf("phase is %q, want crashing", status.Phase)
	}
	if !strings.Contains(status.Detail, "exits") {
		t.Fatalf("detail does not explain the problem: %q", status.Detail)
	}
	if len(status.Instances) != 1 {
		t.Fatalf("got %d instances, want 1", len(status.Instances))
	}
	if status.Instances[0].Restarts != 7 {
		t.Fatalf("restart count is %d, want 7", status.Instances[0].Restarts)
	}
	if status.Instances[0].Status != "CrashLoopBackOff" {
		t.Fatalf("instance status is %q, want CrashLoopBackOff", status.Instances[0].Status)
	}
}

func TestAppStatusForMissingDeployment(t *testing.T) {
	c := &Client{clientset: fake.NewSimpleClientset(), systemNamespace: "skifity-system"}
	status, err := c.AppStatus(t.Context(), "ns", "web")
	if err != nil {
		t.Fatalf("a missing Deployment should not be an error: %v", err)
	}
	if status.Phase != "not_deployed" {
		t.Fatalf("phase is %q, want not_deployed", status.Phase)
	}
}

func TestAppStatusPhases(t *testing.T) {
	cases := []struct {
		name           string
		replicas       int32
		ready          int32
		podReason      string
		wantPhase      string
		detailContains string
	}{
		{"running", 2, 2, "", "running", "running"},
		{"updating", 3, 1, "", "updating", "1 of 3"},
		{"stopped", 0, 0, "", "stopped", "zero"},
		{"pending", 1, 0, "Pending", "pending", "free CPU"},
		{"image pull", 1, 0, "ImagePullBackOff", "failed", "could not be pulled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objects := []any{}
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "ns"},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr(tc.replicas),
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "web"}},
				},
				Status: appsv1.DeploymentStatus{Replicas: tc.replicas, ReadyReplicas: tc.ready},
			}
			objects = append(objects, deployment)
			if tc.podReason != "" {
				objects = append(objects, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "web-1", Namespace: "ns",
						Labels: map[string]string{"app.kubernetes.io/name": "web"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						ContainerStatuses: []corev1.ContainerStatus{{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{Reason: tc.podReason, Message: "detail"},
							},
						}},
					},
				})
			}
			c := &Client{clientset: fake.NewSimpleClientset(toObjects(objects)...), systemNamespace: "skifity-system"}
			status, err := c.AppStatus(t.Context(), "ns", "web")
			if err != nil {
				t.Fatalf("AppStatus: %v", err)
			}
			if status.Phase != tc.wantPhase {
				t.Fatalf("phase is %q, want %q (detail: %q)", status.Phase, tc.wantPhase, status.Detail)
			}
			if tc.detailContains != "" && !strings.Contains(status.Detail, tc.detailContains) {
				t.Fatalf("detail %q does not mention %q", status.Detail, tc.detailContains)
			}
		})
	}
}

func TestDeleteNamespaceRefusesForeignNamespaces(t *testing.T) {
	// A bug or a crafted id must not be able to delete kube-system.
	foreign := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}
	ours := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "acme-shop-production",
		Labels: map[string]string{"app.kubernetes.io/managed-by": "skifity"},
	}}
	c := &Client{clientset: fake.NewSimpleClientset(foreign, ours), systemNamespace: "skifity-system"}

	if err := c.DeleteNamespace(t.Context(), "kube-system"); err == nil {
		t.Fatal("the client agreed to delete kube-system")
	}
	if _, err := c.clientset.CoreV1().Namespaces().Get(t.Context(), "kube-system", metav1.GetOptions{}); err != nil {
		t.Fatalf("kube-system was deleted anyway: %v", err)
	}

	if err := c.DeleteNamespace(t.Context(), "acme-shop-production"); err != nil {
		t.Fatalf("deleting our own namespace failed: %v", err)
	}
	// Deleting one that is already gone must succeed, so cleanup can be retried.
	if err := c.DeleteNamespace(t.Context(), "never-existed"); err != nil {
		t.Fatalf("deleting a missing namespace returned an error: %v", err)
	}
}

func TestRestartAppPatchesPodTemplate(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{}},
		},
	}
	c := &Client{clientset: fake.NewSimpleClientset(deployment), systemNamespace: "skifity-system"}
	if err := c.RestartApp(t.Context(), "ns", "web"); err != nil {
		t.Fatalf("RestartApp: %v", err)
	}
	updated, err := c.clientset.AppsV1().Deployments("ns").Get(t.Context(), "web", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// The annotation on the pod template is what makes Kubernetes roll the
	// pods; an annotation on the Deployment itself would do nothing.
	if updated.Spec.Template.Annotations["skifity.io/restarted-at"] == "" {
		t.Fatal("no restart annotation was set on the pod template")
	}
}

func TestWaitForRolloutDetectsStall(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "ns", Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr(int32(2))},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2,
			Conditions: []appsv1.DeploymentCondition{{
				Type:    appsv1.DeploymentProgressing,
				Status:  corev1.ConditionFalse,
				Message: "ReplicaSet has timed out progressing",
			}},
		},
	}
	c := &Client{clientset: fake.NewSimpleClientset(deployment), systemNamespace: "skifity-system"}
	err := c.WaitForRollout(t.Context(), "ns", "web", 5*time.Second)
	if err == nil {
		t.Fatal("a stalled rollout was reported as successful")
	}
	if !strings.Contains(err.Error(), "timed out progressing") {
		t.Fatalf("the error does not carry the reason: %v", err)
	}
}

func TestWaitForRolloutWaitsForObservedGeneration(t *testing.T) {
	// The status here belongs to the previous revision: the controller has not
	// seen generation 2 yet. Reporting success now would mean a deploy that has
	// not happened is reported as done.
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "ns", Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr(int32(1))},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
		},
	}
	c := &Client{clientset: fake.NewSimpleClientset(deployment), systemNamespace: "skifity-system"}
	if err := c.WaitForRollout(t.Context(), "ns", "web", 3*time.Second); err == nil {
		t.Fatal("the previous revision's status was accepted as the new one's")
	}
}

func TestDescribeNodeReadsRolesAndAddresses(t *testing.T) {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Labels: map[string]string{
				"node-role.kubernetes.io/control-plane": "true",
				"node-role.kubernetes.io/etcd":          "true",
			},
		},
		Spec: corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.5"},
				{Type: corev1.NodeExternalIP, Address: "203.0.113.5"},
			},
			Conditions: []corev1.NodeCondition{{
				Type: corev1.NodeReady, Status: corev1.ConditionFalse, Message: "kubelet stopped posting status",
			}},
		},
	}
	info := describeNode(node)
	if info.Ready {
		t.Fatal("a NotReady node was reported as ready")
	}
	if info.Reason == "" {
		t.Fatal("the reason a node is not ready was dropped, which is what a user needs")
	}
	if info.InternalIP != "10.0.0.5" || info.ExternalIP != "203.0.113.5" {
		t.Fatalf("addresses are %q and %q", info.InternalIP, info.ExternalIP)
	}
	if len(info.Roles) != 2 || info.Roles[0] != "control-plane" {
		t.Fatalf("roles are %v, want control-plane and etcd sorted", info.Roles)
	}
	if info.Schedulable {
		t.Fatal("a cordoned node was reported as schedulable")
	}

	// A node with no role labels is a worker.
	plain := describeNode(corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}})
	if len(plain.Roles) != 1 || plain.Roles[0] != "worker" {
		t.Fatalf("a node with no role labels has roles %v, want worker", plain.Roles)
	}
}

func TestSplitYAML(t *testing.T) {
	manifest := []byte(`# a leading comment
---
apiVersion: v1
kind: Namespace
metadata:
  name: one
---
# just a comment between documents
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: two
data:
  script: |
    echo "---"
    echo done
---
`)
	docs, err := SplitYAML(manifest)
	if err != nil {
		t.Fatalf("SplitYAML: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d documents, want 2", len(docs))
	}
	if docs[0].GetKind() != "Namespace" || docs[1].GetKind() != "ConfigMap" {
		t.Fatalf("kinds are %q and %q", docs[0].GetKind(), docs[1].GetKind())
	}
	// An indented "---" inside a block scalar must not split the document.
	data, found, err := unstructuredString(docs[1], "data", "script")
	if err != nil || !found {
		t.Fatalf("the ConfigMap lost its script: %v", err)
	}
	if !strings.Contains(data, `echo "---"`) {
		t.Fatalf("the block scalar was split at an indented separator: %q", data)
	}
}
