package kube

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"

	"skifity/internal/version"
)

// Client talks to the Kubernetes API.
type Client struct {
	clientset kubernetes.Interface
	dynamic   dynamic.Interface
	metrics   metricsv.Interface
	applier   *Applier
	// systemNamespace is where the panel and the components it installs live.
	systemNamespace string
	config          *rest.Config
}

// Options configure a Client.
type Options struct {
	// KubeconfigPath is empty when running inside the cluster, where the
	// ServiceAccount token is used instead.
	KubeconfigPath  string
	SystemNamespace string
}

// NewClient connects to Kubernetes.
func NewClient(opts Options) (*Client, error) {
	config, err := buildConfig(opts.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	// The panel makes many small calls; the client-go defaults (5 QPS) throttle
	// a deploy noticeably.
	config.QPS = 50
	config.Burst = 100
	config.UserAgent = version.UserAgent()
	config.Timeout = 30 * time.Second

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("build Kubernetes client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("build dynamic Kubernetes client: %w", err)
	}
	// Metrics are optional: a cluster without metrics-server still works, it
	// just cannot show usage or autoscale.
	metricsClient, err := metricsv.NewForConfig(config)
	if err != nil {
		metricsClient = nil
	}

	systemNamespace := opts.SystemNamespace
	if systemNamespace == "" {
		systemNamespace = "skifity-system"
	}
	return &Client{
		clientset:       clientset,
		dynamic:         dyn,
		metrics:         metricsClient,
		applier:         NewApplier(dyn),
		systemNamespace: systemNamespace,
		config:          config,
	}, nil
}

func buildConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("read kubeconfig %s: %w", kubeconfigPath, err)
		}
		return config, nil
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("the panel is not running inside a cluster and no kubeconfig was configured: %w", err)
	}
	return config, nil
}

// Applier exposes the server-side apply helper to the orchestrators.
func (c *Client) Applier() *Applier { return c.applier }

// Clientset exposes the typed client for the few places that need it.
func (c *Client) Clientset() kubernetes.Interface { return c.clientset }

// SystemNamespace is where the panel and its components run.
func (c *Client) SystemNamespace() string { return c.systemNamespace }

// RESTConfig exposes the connection settings, which exec and port-forward need.
func (c *Client) RESTConfig() *rest.Config { return c.config }

// Ping reports whether the Kubernetes API is reachable.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Listing one namespace is the cheapest call that proves both connectivity
	// and that our credentials still work.
	_, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("reach the Kubernetes API: %w", err)
	}
	return nil
}

// Version reports the cluster's Kubernetes version.
func (c *Client) Version(ctx context.Context) (string, error) {
	info, err := c.clientset.Discovery().ServerVersion()
	if err != nil {
		return "", fmt.Errorf("read the cluster version: %w", err)
	}
	return info.GitVersion, nil
}

// nodeUsage is the per-node usage read from metrics-server.
type nodeUsage struct {
	cpuMilli int64
	memoryMB int64
}

// Summary describes the cluster for the dashboard.
func (c *Client) Summary(ctx context.Context) (Summary, error) {
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return Summary{}, fmt.Errorf("list cluster nodes: %w", err)
	}

	usage := c.nodeUsage(ctx)
	podCounts := c.podCountsByNode(ctx)

	out := Summary{Reachable: true}
	if v, err := c.Version(ctx); err == nil {
		out.KubernetesVersion = v
	}

	controlPlanes := 0
	for _, node := range nodes.Items {
		info := describeNode(node)
		if u, ok := usage[node.Name]; ok {
			info.CPUUsedM = u.cpuMilli
			info.MemUsedMB = u.memoryMB
		}
		info.PodCount = podCounts[node.Name]

		out.Nodes = append(out.Nodes, info)
		out.TotalCPUM += info.CPUCapacityM
		out.TotalMemoryMB += info.MemCapacityMB
		out.UsedCPUM += info.CPUUsedM
		out.UsedMemoryMB += info.MemUsedMB
		if info.Ready {
			out.ReadyNodes++
		}
		for _, role := range info.Roles {
			if role == "control-plane" || role == "master" {
				controlPlanes++
			}
		}
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Name < out.Nodes[j].Name })
	// Embedded etcd needs three members to survive losing one.
	out.HighAvailability = controlPlanes >= 3
	return out, nil
}

// Summary mirrors the API layer's ClusterSummary. It is defined here too so the
// kube package does not import the api package, which would be a cycle.
type Summary struct {
	Reachable         bool
	KubernetesVersion string
	Nodes             []Node
	ReadyNodes        int
	TotalCPUM         int64
	TotalMemoryMB     int64
	UsedCPUM          int64
	UsedMemoryMB      int64
	HighAvailability  bool
	Message           string
}

// Node is one cluster node.
type Node struct {
	Name          string
	Ready         bool
	Reason        string
	Roles         []string
	InternalIP    string
	ExternalIP    string
	OS            string
	Architecture  string
	KubeletVer    string
	CPUCapacityM  int64
	MemCapacityMB int64
	CPUUsedM      int64
	MemUsedMB     int64
	PodCount      int
	Labels        map[string]string
	Schedulable   bool
}

func describeNode(node corev1.Node) Node {
	info := Node{
		Name:         node.Name,
		OS:           node.Status.NodeInfo.OSImage,
		Architecture: node.Status.NodeInfo.Architecture,
		KubeletVer:   node.Status.NodeInfo.KubeletVersion,
		Labels:       node.Labels,
		Schedulable:  !node.Spec.Unschedulable,
	}
	if cpu := node.Status.Allocatable.Cpu(); cpu != nil {
		info.CPUCapacityM = cpu.MilliValue()
	}
	if mem := node.Status.Allocatable.Memory(); mem != nil {
		info.MemCapacityMB = mem.Value() / (1024 * 1024)
	}
	for _, addr := range node.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalIP:
			info.InternalIP = addr.Address
		case corev1.NodeExternalIP:
			info.ExternalIP = addr.Address
		}
	}
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			info.Ready = cond.Status == corev1.ConditionTrue
			if !info.Ready {
				// The message is what tells a user why a server went away.
				info.Reason = cond.Message
			}
		}
	}
	for label := range node.Labels {
		if role, ok := strings.CutPrefix(label, "node-role.kubernetes.io/"); ok && role != "" {
			info.Roles = append(info.Roles, role)
		}
	}
	if len(info.Roles) == 0 {
		info.Roles = []string{"worker"}
	}
	sort.Strings(info.Roles)
	return info
}

func (c *Client) nodeUsage(ctx context.Context) map[string]nodeUsage {
	out := map[string]nodeUsage{}
	if c.metrics == nil {
		return out
	}
	list, err := c.metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		// metrics-server may not be ready yet, which is not an error worth
		// failing the whole dashboard for.
		return out
	}
	for _, item := range list.Items {
		out[item.Name] = nodeUsage{
			cpuMilli: item.Usage.Cpu().MilliValue(),
			memoryMB: item.Usage.Memory().Value() / (1024 * 1024),
		}
	}
	return out
}

func (c *Client) podCountsByNode(ctx context.Context) map[string]int {
	out := map[string]int{}
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return out
	}
	for _, pod := range pods.Items {
		out[pod.Spec.NodeName]++
	}
	return out
}

// AppStatus describes one app's live state in the panel's own words.
func (c *Client) AppStatus(ctx context.Context, namespace, appSlug string) (AppStatus, error) {
	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, appSlug, metav1.GetOptions{})
	if err != nil {
		if IsNotFound(err) {
			return AppStatus{Phase: "not_deployed", Detail: "This app has not been deployed yet."}, nil
		}
		return AppStatus{}, fmt.Errorf("read the app's Deployment: %w", err)
	}

	status := AppStatus{
		DesiredReplicas: int(deployment.Status.Replicas),
		ReadyReplicas:   int(deployment.Status.ReadyReplicas),
	}
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		status.Image = deployment.Spec.Template.Spec.Containers[0].Image
	}

	selector := labels.SelectorFromSet(deployment.Spec.Selector.MatchLabels).String()
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return status, fmt.Errorf("list the app's instances: %w", err)
	}
	for _, pod := range pods.Items {
		status.Instances = append(status.Instances, describePod(pod))
	}
	sort.Slice(status.Instances, func(i, j int) bool { return status.Instances[i].Name < status.Instances[j].Name })

	status.Phase, status.Detail = summarisePhase(deployment, status)
	return status, nil
}

// AppStatus mirrors the API layer's AppRuntimeStatus.
type AppStatus struct {
	Phase           string
	Detail          string
	DesiredReplicas int
	ReadyReplicas   int
	Instances       []Instance
	Image           string
}

// Instance is one running pod, described the way the UI shows it.
type Instance struct {
	Name      string
	Status    string
	Ready     bool
	Restarts  int
	Node      string
	StartedAt time.Time
	Message   string
	CPUM      int64
	MemoryMB  int64
}

func describePod(pod corev1.Pod) Instance {
	inst := Instance{Name: pod.Name, Node: pod.Spec.NodeName, Status: string(pod.Status.Phase)}
	if pod.Status.StartTime != nil {
		inst.StartedAt = pod.Status.StartTime.Time
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			inst.Ready = cond.Status == corev1.ConditionTrue
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		inst.Restarts += int(cs.RestartCount)
		switch {
		case cs.State.Waiting != nil:
			// The waiting reason is the single most useful thing on this
			// screen: CrashLoopBackOff, ImagePullBackOff, CreateContainerError.
			inst.Status = cs.State.Waiting.Reason
			inst.Message = cs.State.Waiting.Message
		case cs.State.Terminated != nil:
			inst.Status = cs.State.Terminated.Reason
			inst.Message = cs.State.Terminated.Message
		}
	}
	// A pod that is being deleted shows as Running, which is confusing while a
	// deploy is finishing.
	if pod.DeletionTimestamp != nil {
		inst.Status = "Terminating"
		inst.Ready = false
	}
	return inst
}

// summarisePhase turns the Deployment's conditions into one sentence.
func summarisePhase(deployment *appsv1.Deployment, status AppStatus) (string, string) {
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}

	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
			return "failed", "The rollout did not finish: " + cond.Message
		}
		if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
			return "failed", cond.Message
		}
	}

	switch {
	case desired == 0:
		return "stopped", "This app is scaled to zero instances."
	case status.ReadyReplicas == 0 && len(status.Instances) > 0:
		for _, inst := range status.Instances {
			if strings.Contains(inst.Status, "CrashLoop") {
				return "crashing", "The app starts and then exits. The last log lines usually say why."
			}
			if strings.Contains(inst.Status, "ImagePull") || strings.Contains(inst.Status, "ErrImage") {
				return "failed", "The image could not be pulled: " + inst.Message
			}
			if inst.Status == "Pending" {
				return "pending", "Waiting for a server with enough free CPU and memory."
			}
		}
		return "starting", "The instances are starting."
	case status.ReadyReplicas == 0:
		return "pending", "No instances are running yet."
	case int32(status.ReadyReplicas) < desired:
		return "updating", fmt.Sprintf("%d of %d instances are ready.", status.ReadyReplicas, desired)
	default:
		return "running", fmt.Sprintf("%d instance(s) running.", status.ReadyReplicas)
	}
}

// AppLogs streams the logs of an app's instances.
func (c *Client) AppLogs(ctx context.Context, namespace, appSlug string, tailLines int64, follow bool) (io.ReadCloser, error) {
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"app.kubernetes.io/name": appSlug}).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("find the app's instances: %w", err)
	}
	if len(pods.Items) == 0 {
		return io.NopCloser(strings.NewReader("")), nil
	}

	// Prefer a ready pod: the logs of a pod that is crash-looping are what the
	// user wants when nothing is ready, but noise when something is serving.
	target := pods.Items[0]
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				target = pod
				break
			}
		}
	}

	options := &corev1.PodLogOptions{Follow: follow, Timestamps: false}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	stream, err := c.clientset.CoreV1().Pods(namespace).GetLogs(target.Name, options).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("read logs from %s: %w", target.Name, err)
	}
	return stream, nil
}

// RestartApp triggers a rolling restart without changing anything else.
func (c *Client) RestartApp(ctx context.Context, namespace, appSlug string) error {
	// Changing an annotation on the pod template is how kubectl rollout restart
	// works, and it keeps the rollout ordinary: the same surge and readiness
	// rules apply, so a restart is still zero downtime.
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"%s":%q}}}}}`,
		version.LabelKey("restarted-at"), time.Now().UTC().Format(time.RFC3339))
	_, err := c.clientset.AppsV1().Deployments(namespace).Patch(
		ctx, appSlug, "application/strategic-merge-patch+json", []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("restart %s: %w", appSlug, err)
	}
	return nil
}

// WaitForRollout blocks until a Deployment's new revision is fully available.
func (c *Client) WaitForRollout(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastMessage string
	for {
		deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read the rollout status: %w", err)
		}
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		// Comparing the observed generation stops us reporting success from
		// the previous revision's status before the controller has caught up.
		if deployment.Status.ObservedGeneration >= deployment.Generation &&
			deployment.Status.UpdatedReplicas == desired &&
			deployment.Status.ReadyReplicas == desired &&
			deployment.Status.UnavailableReplicas == 0 {
			return nil
		}
		for _, cond := range deployment.Status.Conditions {
			if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
				return fmt.Errorf("the rollout stopped making progress: %s", cond.Message)
			}
			if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
				lastMessage = cond.Message
			}
		}

		if time.Now().After(deadline) {
			if lastMessage == "" {
				lastMessage = fmt.Sprintf("%d of %d instances became ready",
					deployment.Status.ReadyReplicas, desired)
			}
			return fmt.Errorf("the rollout did not finish in %s: %s", timeout, lastMessage)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// DeleteApp removes everything an app owns in its namespace.
func (c *Client) DeleteApp(ctx context.Context, namespace, appSlug string) error {
	// Ordered so that traffic stops before the workload does.
	for _, target := range []struct{ apiVersion, kind, name string }{
		{"networking.k8s.io/v1", "Ingress", appSlug},
		{"autoscaling/v2", "HorizontalPodAutoscaler", ResourceName(appSlug, "hpa")},
		{"policy/v1", "PodDisruptionBudget", ResourceName(appSlug, "pdb")},
		{"v1", "Service", appSlug},
		{"apps/v1", "Deployment", appSlug},
		{"v1", "Secret", ResourceName(appSlug, "env")},
	} {
		if err := c.applier.Delete(ctx, target.apiVersion, target.kind, namespace, target.name); err != nil {
			return err
		}
	}
	// PersistentVolumeClaims are deliberately left behind: deleting an app
	// should not silently destroy its data. They are removed with the
	// environment, or by hand.
	return nil
}

// EnsureNamespace creates an environment's namespace with its guards.
func (c *Client) EnsureNamespace(ctx context.Context, namespace, teamID, projectID string) error {
	objects := []any{
		BuildNamespace(namespace, teamID, projectID),
		BuildLimitRange(namespace),
		BuildResourceQuota(namespace, DefaultQuota()),
	}
	for _, policy := range BuildNetworkPolicies(namespace, c.systemNamespace) {
		objects = append(objects, policy)
	}
	if err := c.applier.ApplyAll(ctx, objects...); err != nil {
		return fmt.Errorf("prepare the namespace %s: %w", namespace, err)
	}
	return nil
}

// DeleteNamespace removes an environment and everything in it.
func (c *Client) DeleteNamespace(ctx context.Context, namespace string) error {
	// Refusing to delete a namespace we do not own stops a bug or a crafted id
	// from removing kube-system.
	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("read the namespace %s: %w", namespace, err)
	}
	if ns.Labels["app.kubernetes.io/managed-by"] != version.Binary {
		return fmt.Errorf("refusing to delete the namespace %s: it was not created by %s", namespace, version.Name)
	}
	if err := c.clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{}); err != nil && !IsNotFound(err) {
		return fmt.Errorf("delete the namespace %s: %w", namespace, err)
	}
	return nil
}
