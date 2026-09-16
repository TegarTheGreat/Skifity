// Package mcpserver exposes the panel to AI assistants over the Model Context
// Protocol.
//
// The tools here are the same operations the CLI and the panel offer, going
// through the same API with the same scoped token. An assistant can therefore
// do exactly what the person who created the token can do, and nothing else.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"skifity/internal/cli"
	"skifity/internal/errdoc"
	"skifity/internal/store"
	"skifity/internal/version"
)

// Server wraps an API client as an MCP server.
type Server struct {
	client *cli.Client
	config cli.Config
	mcp    *mcp.Server

	// The team is worked out on first use and kept, because every tool that
	// does anything needs it and the answer does not change.
	teamOnce sync.Once
	team     string
	teamErr  error
}

// New builds an MCP server for a panel.
func New(cfg cli.Config) *Server {
	s := &Server{client: cli.NewClient(cfg), config: cfg}
	s.mcp = mcp.NewServer(&mcp.Implementation{
		Name:    version.Binary,
		Version: version.Version,
		Title:   version.Name,
	}, &mcp.ServerOptions{
		Instructions: strings.TrimSpace(`
` + version.Name + ` runs applications on a Kubernetes cluster the user owns.

Use list_apps to find an app's id before calling anything that takes one.
Deploys are asynchronous: deploy_app returns immediately, and get_app_status
tells you what happened. When something fails, the error carries a cause, an
impact and a suggested fix; relay all three rather than only the first line.

Changing an environment variable or the instance count does not rebuild the
app, so those are cheap. Deploying a new commit does rebuild and takes minutes.
`),
	})
	s.register()
	return s
}

// Run serves the protocol over stdin and stdout.
func (s *Server) Run(ctx context.Context) error {
	if err := s.mcp.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("run the MCP server: %w", err)
	}
	return nil
}

// --- tool inputs and outputs ---

type listProjectsOutput struct {
	Projects []projectSummary `json:"projects"`
}

type projectSummary struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Environments []environmentSummary `json:"environments"`
}

type environmentSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Kind string `json:"kind"`
}

type createAppInput struct {
	Name          string `json:"name" jsonschema:"what to call the app"`
	EnvironmentID string `json:"environment_id,omitempty" jsonschema:"where to create it; omitted means the default environment"`
	RepoURL       string `json:"repo_url,omitempty" jsonschema:"an https git repository to build from"`
	Branch        string `json:"branch,omitempty" jsonschema:"the branch to deploy; defaults to the repository's own default"`
	Image         string `json:"image,omitempty" jsonschema:"a prebuilt container image to run instead of building from a repository"`
	Port          int    `json:"port,omitempty" jsonschema:"the port the app listens on; defaults to 8080, which is also what PORT is set to"`
	Deploy        bool   `json:"deploy,omitempty" jsonschema:"start the first deployment straight away"`
}

type createAppOutput struct {
	AppID        string `json:"app_id"`
	Name         string `json:"name"`
	URL          string `json:"url,omitempty"`
	DeploymentID string `json:"deployment_id,omitempty"`
	Note         string `json:"note"`
}

type listAppsInput struct {
	EnvironmentID string `json:"environment_id,omitempty" jsonschema:"the environment to list; omitted means the default one"`
}

type appSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	SourceType string `json:"source_type"`
	Repository string `json:"repository,omitempty"`
	Image      string `json:"image,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Instances  int    `json:"instances"`
	Autoscale  bool   `json:"autoscale"`
}

type listAppsOutput struct {
	Apps []appSummary `json:"apps"`
}

type appIDInput struct {
	AppID string `json:"app_id" jsonschema:"the app's id, as returned by list_apps"`
}

type statusOutput struct {
	Phase           string   `json:"phase"`
	Explanation     string   `json:"explanation"`
	ReadyInstances  int      `json:"ready_instances"`
	WantedInstances int      `json:"wanted_instances"`
	URLs            []string `json:"urls,omitempty"`
	Instances       []struct {
		Name     string `json:"name"`
		Status   string `json:"status"`
		Ready    bool   `json:"ready"`
		Restarts int    `json:"restarts"`
		Node     string `json:"node"`
	} `json:"instances,omitempty"`
}

type deployInput struct {
	AppID     string `json:"app_id" jsonschema:"the app's id"`
	CommitSHA string `json:"commit_sha,omitempty" jsonschema:"a specific commit; omitted means the tip of the configured branch"`
	Force     bool   `json:"force,omitempty" jsonschema:"rebuild even when nothing about the build has changed"`
}

type deployOutput struct {
	DeploymentID string `json:"deployment_id"`
	Number       int    `json:"number"`
	Status       string `json:"status"`
	Note         string `json:"note"`
}

type logsInput struct {
	AppID string `json:"app_id" jsonschema:"the app's id"`
	Lines int    `json:"lines,omitempty" jsonschema:"how many lines to return, up to 500"`
}

type logsOutput struct {
	Lines []string `json:"lines"`
}

type listVariablesOutput struct {
	Variables []struct {
		Key      string `json:"key"`
		Value    string `json:"value,omitempty"`
		IsSecret bool   `json:"is_secret"`
	} `json:"variables"`
	Note string `json:"note"`
}

type setVariableInput struct {
	AppID     string `json:"app_id" jsonschema:"the app's id"`
	Key       string `json:"key" jsonschema:"the variable's name, in CAPITALS_WITH_UNDERSCORES"`
	Value     string `json:"value" jsonschema:"the value"`
	IsSecret  bool   `json:"is_secret,omitempty" jsonschema:"store it encrypted and never show it again"`
	BuildTime bool   `json:"build_time,omitempty" jsonschema:"the value is needed while building, so setting it causes a rebuild"`
}

type setVariableOutput struct {
	Key             string `json:"key"`
	RequiresRebuild bool   `json:"requires_rebuild"`
	Note            string `json:"note"`
}

type scaleInput struct {
	AppID     string `json:"app_id" jsonschema:"the app's id"`
	Instances int    `json:"instances,omitempty" jsonschema:"a fixed number of instances"`
	Autoscale bool   `json:"autoscale,omitempty" jsonschema:"scale automatically between min and max"`
	Min       int    `json:"min,omitempty" jsonschema:"the fewest instances when autoscaling"`
	Max       int    `json:"max,omitempty" jsonschema:"the most instances when autoscaling"`
	CPUTarget int    `json:"cpu_target,omitempty" jsonschema:"the CPU percentage to scale on, 1 to 100"`
}

type scaleOutput struct {
	Applied  map[string]any `json:"applied"`
	Warnings []struct {
		Severity string `json:"severity"`
		Title    string `json:"title"`
		Detail   string `json:"detail"`
		Fix      string `json:"fix"`
	} `json:"warnings,omitempty"`
}

type rollbackInput struct {
	AppID        string `json:"app_id" jsonschema:"the app's id"`
	DeploymentID string `json:"deployment_id,omitempty" jsonschema:"which deployment to go back to; omitted means the one before the current version"`
}

type historyOutput struct {
	Deployments []struct {
		ID      string `json:"id"`
		Number  int    `json:"number"`
		Status  string `json:"status"`
		Commit  string `json:"commit,omitempty"`
		Error   string `json:"error,omitempty"`
		Hint    string `json:"hint,omitempty"`
		Created string `json:"created"`
	} `json:"deployments"`
}

type clusterOutput struct {
	Reachable         bool            `json:"reachable"`
	KubernetesVer     string          `json:"kubernetes_version,omitempty"`
	ReadyServers      int             `json:"ready_servers"`
	TotalServers      int             `json:"total_servers"`
	HighAvailability  bool            `json:"high_availability"`
	CPUUsedPercent    int             `json:"cpu_used_percent"`
	MemoryUsedPercent int             `json:"memory_used_percent"`
	Servers           []serverSummary `json:"servers"`
}

// serverSummary is one cluster node as an assistant sees it.
type serverSummary struct {
	Name   string   `json:"name"`
	Ready  bool     `json:"ready"`
	Roles  []string `json:"roles"`
	Reason string   `json:"reason,omitempty"`
}

type readinessOutput struct {
	Findings []struct {
		Severity string `json:"severity"`
		Title    string `json:"title"`
		Detail   string `json:"detail"`
		Fix      string `json:"fix"`
	} `json:"findings"`
	Summary string `json:"summary"`
}

// register wires up the tools.
func (s *Server) register() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_projects",
		Description: "List the projects in this team and the environments inside them. Use this to find an environment id before creating an app.",
	}, s.listProjects)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "create_app",
		Description: "Create an application from a git repository or a prebuilt image, and optionally deploy it. This is how a new app gets onto the cluster.",
	}, s.createApp)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_apps",
		Description: "List the applications in an environment, with their ids and current state. Call this first: every other tool takes an app id.",
	}, s.listApps)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_app_status",
		Description: "Describe an app's live state: whether it is running, how many instances are ready, and its URLs. Use this after a deploy to find out what happened.",
	}, s.getAppStatus)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "deploy_app",
		Description: "Start a deployment. This returns immediately; the build takes minutes. Poll get_app_status or call get_deployment_history to see the outcome.",
	}, s.deployApp)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_app_logs",
		Description: "Read an app's recent log lines. This is where the cause of a crash or a failed start is.",
	}, s.getAppLogs)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_variables",
		Description: "List an app's environment variables. Values marked as secrets are not returned: they are write-only once set.",
	}, s.listVariables)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "set_variable",
		Description: "Set an environment variable. A runtime variable is rolled out without rebuilding; a build-time variable causes a rebuild on the next deploy.",
	}, s.setVariable)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "scale_app",
		Description: "Change how many instances an app runs, or turn on autoscaling. Returns any reason the app may not behave correctly with several instances.",
	}, s.scaleApp)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "rollback_app",
		Description: "Go back to a previous deployment. This restores the image and the settings that version ran with.",
	}, s.rollbackApp)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_deployment_history",
		Description: "List an app's recent deployments with their outcome, and the reason and suggested fix for any that failed.",
	}, s.getHistory)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_cluster_status",
		Description: "Describe the servers in the cluster: how many are ready, whether it survives losing one, and how much capacity is left.",
	}, s.getCluster)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "check_scaling_readiness",
		Description: "Report what would break if this app ran more than one instance, such as local file storage or in-memory sessions, with a suggested fix for each.",
	}, s.checkReadiness)
}

// --- handlers ---

func (s *Server) listProjects(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listProjectsOutput, error) {
	teamID, err := s.teamID(ctx)
	if err != nil {
		return errorResult(err), listProjectsOutput{}, nil
	}
	var projects struct {
		Items []store.Project `json:"items"`
	}
	if err := s.client.Do(ctx, "GET", "/api/teams/"+teamID+"/projects", nil, &projects); err != nil {
		return errorResult(err), listProjectsOutput{}, nil
	}

	out := listProjectsOutput{Projects: make([]projectSummary, 0, len(projects.Items))}
	for _, project := range projects.Items {
		summary := projectSummary{ID: project.ID, Name: project.Name}
		var environments struct {
			Items []store.Environment `json:"items"`
		}
		if err := s.client.Do(ctx, "GET",
			"/api/projects/"+project.ID+"/environments", nil, &environments); err != nil {
			return errorResult(err), listProjectsOutput{}, nil
		}
		for _, env := range environments.Items {
			summary.Environments = append(summary.Environments, environmentSummary{
				ID: env.ID, Name: env.Name, Slug: env.Slug, Kind: string(env.Kind),
			})
		}
		out.Projects = append(out.Projects, summary)
	}
	return textResult(fmt.Sprintf("%d project(s).", len(out.Projects))), out, nil
}

func (s *Server) createApp(ctx context.Context, _ *mcp.CallToolRequest, in createAppInput) (*mcp.CallToolResult, createAppOutput, error) {
	environment := in.EnvironmentID
	if environment == "" {
		resolved, err := s.defaultEnvironment(ctx)
		if err != nil {
			return errorResult(err), createAppOutput{}, nil
		}
		environment = resolved
	}

	body := map[string]any{"name": in.Name, "deploy": in.Deploy}
	switch {
	case in.Image != "":
		body["source_type"] = "image"
		body["image"] = in.Image
	case in.RepoURL != "":
		body["source_type"] = "git"
		body["repo_url"] = in.RepoURL
		if in.Branch != "" {
			body["branch"] = in.Branch
		}
	default:
		return errorResult(errdoc.BadRequest(
			"An app needs either a repository to build from or an image to run.")), createAppOutput{}, nil
	}
	if in.Port > 0 {
		body["port"] = in.Port
	}

	// The panel answers with the app alone, or — when a deployment was asked
	// for and started — with the app nested next to it. Both shapes are read
	// from one struct rather than guessing which arrived.
	var response struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		App        *store.App        `json:"app"`
		Deployment *store.Deployment `json:"deployment"`
	}
	if err := s.client.Do(ctx, "POST", "/api/environments/"+environment+"/apps", body, &response); err != nil {
		return errorResult(err), createAppOutput{}, nil
	}
	out := createAppOutput{AppID: response.ID, Name: response.Name}
	if response.App != nil {
		out.AppID, out.Name = response.App.ID, response.App.Name
	}
	if response.Deployment != nil {
		out.DeploymentID = response.Deployment.ID
	}
	switch {
	case out.DeploymentID != "":
		out.Note = "The first deployment has started. The build takes a few minutes; " +
			"call get_app_status or get_deployment_history to see the outcome."
	default:
		out.Note = "The app exists but has never been deployed. Call deploy_app to start it."
	}
	return textResult(fmt.Sprintf("Created %s (%s). %s", out.Name, out.AppID, out.Note)), out, nil
}

func (s *Server) listApps(ctx context.Context, _ *mcp.CallToolRequest, in listAppsInput) (*mcp.CallToolResult, listAppsOutput, error) {
	environment := in.EnvironmentID
	if environment == "" {
		resolved, err := s.defaultEnvironment(ctx)
		if err != nil {
			return errorResult(err), listAppsOutput{}, nil
		}
		environment = resolved
	}

	var response struct {
		Items []store.App `json:"items"`
	}
	if err := s.client.Do(ctx, "GET", "/api/environments/"+environment+"/apps", nil, &response); err != nil {
		return errorResult(err), listAppsOutput{}, nil
	}

	out := listAppsOutput{Apps: make([]appSummary, 0, len(response.Items))}
	for _, app := range response.Items {
		out.Apps = append(out.Apps, appSummary{
			ID: app.ID, Name: app.Name, Status: app.Status, SourceType: app.SourceType,
			Repository: app.RepoURL, Image: app.Image, Branch: app.Branch,
			Instances: app.Replicas, Autoscale: app.Autoscale,
		})
	}
	return textResult(fmt.Sprintf("%d app(s).", len(out.Apps))), out, nil
}

func (s *Server) getAppStatus(ctx context.Context, _ *mcp.CallToolRequest, in appIDInput) (*mcp.CallToolResult, statusOutput, error) {
	var raw struct {
		Phase           string   `json:"phase"`
		Detail          string   `json:"detail"`
		DesiredReplicas int      `json:"desired_replicas"`
		ReadyReplicas   int      `json:"ready_replicas"`
		URLs            []string `json:"urls"`
		Instances       []struct {
			Name     string `json:"name"`
			Status   string `json:"status"`
			Ready    bool   `json:"ready"`
			Restarts int    `json:"restarts"`
			Node     string `json:"node"`
		} `json:"instances"`
	}
	if err := s.client.Do(ctx, "GET", "/api/apps/"+in.AppID+"/status", nil, &raw); err != nil {
		return errorResult(err), statusOutput{}, nil
	}

	out := statusOutput{
		Phase: raw.Phase, Explanation: raw.Detail,
		ReadyInstances: raw.ReadyReplicas, WantedInstances: raw.DesiredReplicas,
		URLs: raw.URLs, Instances: raw.Instances,
	}
	summary := fmt.Sprintf("%s: %d of %d instances ready. %s",
		raw.Phase, raw.ReadyReplicas, raw.DesiredReplicas, raw.Detail)
	return textResult(summary), out, nil
}

func (s *Server) deployApp(ctx context.Context, _ *mcp.CallToolRequest, in deployInput) (*mcp.CallToolResult, deployOutput, error) {
	body := map[string]any{"force": in.Force}
	if in.CommitSHA != "" {
		body["commit_sha"] = in.CommitSHA
	}
	var deployment store.Deployment
	if err := s.client.Do(ctx, "POST", "/api/apps/"+in.AppID+"/deploy", body, &deployment); err != nil {
		return errorResult(err), deployOutput{}, nil
	}

	note := "The build runs in the background and usually takes a few minutes. " +
		"Call get_app_status or get_deployment_history to see the outcome."
	if deployment.Image != "" {
		note = "Nothing needed building: this version was already built, so the existing image " +
			"is being rolled out. This is fast."
	}
	out := deployOutput{
		DeploymentID: deployment.ID, Number: deployment.Number,
		Status: string(deployment.Status), Note: note,
	}
	return textResult(fmt.Sprintf("Deployment #%d started. %s", deployment.Number, note)), out, nil
}

func (s *Server) getAppLogs(ctx context.Context, _ *mcp.CallToolRequest, in logsInput) (*mcp.CallToolResult, logsOutput, error) {
	lines := in.Lines
	if lines <= 0 || lines > 500 {
		lines = 200
	}
	var response struct {
		Lines []string `json:"lines"`
	}
	path := fmt.Sprintf("/api/apps/%s/logs?tail=%d", in.AppID, lines)
	if err := s.client.Do(ctx, "GET", path, nil, &response); err != nil {
		return errorResult(err), logsOutput{}, nil
	}
	out := logsOutput{Lines: response.Lines}
	return textResult(strings.Join(response.Lines, "\n")), out, nil
}

func (s *Server) listVariables(ctx context.Context, _ *mcp.CallToolRequest, in appIDInput) (*mcp.CallToolResult, listVariablesOutput, error) {
	var response struct {
		Items []store.Variable `json:"items"`
	}
	if err := s.client.Do(ctx, "GET", "/api/apps/"+in.AppID+"/variables", nil, &response); err != nil {
		return errorResult(err), listVariablesOutput{}, nil
	}

	var out listVariablesOutput
	for _, variable := range response.Items {
		out.Variables = append(out.Variables, struct {
			Key      string `json:"key"`
			Value    string `json:"value,omitempty"`
			IsSecret bool   `json:"is_secret"`
		}{Key: variable.Key, Value: variable.Value, IsSecret: variable.IsSecret})
	}
	out.Note = "Secrets are stored encrypted and are never returned. You can set a new value but not read the current one."
	return textResult(fmt.Sprintf("%d variable(s).", len(out.Variables))), out, nil
}

func (s *Server) setVariable(ctx context.Context, _ *mcp.CallToolRequest, in setVariableInput) (*mcp.CallToolResult, setVariableOutput, error) {
	body := map[string]any{
		"key": in.Key, "value": in.Value,
		"is_secret": in.IsSecret, "build_time": in.BuildTime,
	}
	var response struct {
		RequiresRebuild bool `json:"requires_rebuild"`
	}
	if err := s.client.Do(ctx, "PUT", "/api/apps/"+in.AppID+"/variables", body, &response); err != nil {
		return errorResult(err), setVariableOutput{}, nil
	}

	note := "Rolled out to the running instances without rebuilding."
	if response.RequiresRebuild {
		note = "This value is used during the build, so the next deploy will rebuild the image."
	}
	out := setVariableOutput{Key: in.Key, RequiresRebuild: response.RequiresRebuild, Note: note}
	return textResult(in.Key + " set. " + note), out, nil
}

func (s *Server) scaleApp(ctx context.Context, _ *mcp.CallToolRequest, in scaleInput) (*mcp.CallToolResult, scaleOutput, error) {
	body := map[string]any{}
	if in.Autoscale {
		body["autoscale"] = true
		if in.Min > 0 {
			body["min_replicas"] = in.Min
		}
		if in.Max > 0 {
			body["max_replicas"] = in.Max
		}
		if in.CPUTarget > 0 {
			body["cpu_target"] = in.CPUTarget
		}
	} else if in.Instances > 0 {
		body["replicas"] = in.Instances
		body["autoscale"] = false
	} else {
		return errorResult(errdoc.BadRequest(
			"Give either instances, or autoscale with a min and a max.")), scaleOutput{}, nil
	}

	var response struct {
		Scaling  map[string]any `json:"scaling"`
		Warnings []struct {
			Severity string `json:"severity"`
			Title    string `json:"title"`
			Detail   string `json:"detail"`
			Fix      string `json:"fix"`
		} `json:"warnings"`
	}
	if err := s.client.Do(ctx, "PUT", "/api/apps/"+in.AppID+"/scaling", body, &response); err != nil {
		return errorResult(err), scaleOutput{}, nil
	}

	out := scaleOutput{Applied: response.Scaling, Warnings: response.Warnings}
	summary := "Scaling updated."
	if len(response.Warnings) > 0 {
		summary += fmt.Sprintf(" %d thing(s) may stop this app working correctly with several instances; see warnings.",
			len(response.Warnings))
	}
	return textResult(summary), out, nil
}

func (s *Server) rollbackApp(ctx context.Context, _ *mcp.CallToolRequest, in rollbackInput) (*mcp.CallToolResult, deployOutput, error) {
	target := in.DeploymentID
	if target == "" {
		resolved, err := s.previousDeployment(ctx, in.AppID)
		if err != nil {
			return errorResult(err), deployOutput{}, nil
		}
		target = resolved
	}

	var deployment store.Deployment
	if err := s.client.Do(ctx, "POST", "/api/apps/"+in.AppID+"/rollback/"+target, nil, &deployment); err != nil {
		return errorResult(err), deployOutput{}, nil
	}
	out := deployOutput{
		DeploymentID: deployment.ID, Number: deployment.Number,
		Status: string(deployment.Status),
		Note:   "The previous image and its settings are being rolled out. No rebuild is needed, so this is fast.",
	}
	return textResult(fmt.Sprintf("Rolling back. Deployment #%d started.", deployment.Number)), out, nil
}

func (s *Server) getHistory(ctx context.Context, _ *mcp.CallToolRequest, in appIDInput) (*mcp.CallToolResult, historyOutput, error) {
	var response struct {
		Items []store.Deployment `json:"items"`
	}
	if err := s.client.Do(ctx, "GET", "/api/apps/"+in.AppID+"/deployments?limit=20", nil, &response); err != nil {
		return errorResult(err), historyOutput{}, nil
	}

	var out historyOutput
	for _, deployment := range response.Items {
		out.Deployments = append(out.Deployments, struct {
			ID      string `json:"id"`
			Number  int    `json:"number"`
			Status  string `json:"status"`
			Commit  string `json:"commit,omitempty"`
			Error   string `json:"error,omitempty"`
			Hint    string `json:"hint,omitempty"`
			Created string `json:"created"`
		}{
			ID: deployment.ID, Number: deployment.Number, Status: string(deployment.Status),
			Commit: deployment.CommitSHA, Error: deployment.ErrorMessage,
			Hint: deployment.ErrorHint, Created: deployment.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return textResult(fmt.Sprintf("%d deployment(s).", len(out.Deployments))), out, nil
}

func (s *Server) getCluster(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, clusterOutput, error) {
	teamID, err := s.teamID(ctx)
	if err != nil {
		return errorResult(err), clusterOutput{}, nil
	}
	var summary struct {
		Reachable        bool            `json:"reachable"`
		KubernetesVer    string          `json:"kubernetes_version"`
		ReadyNodes       int             `json:"ready_nodes"`
		TotalCPUM        int64           `json:"total_cpu_m"`
		TotalMemoryMB    int64           `json:"total_memory_mb"`
		UsedCPUM         int64           `json:"used_cpu_m"`
		UsedMemoryMB     int64           `json:"used_memory_mb"`
		HighAvailability bool            `json:"high_availability"`
		Nodes            []serverSummary `json:"nodes"`
	}
	if err := s.client.Do(ctx, "GET", "/api/teams/"+teamID+"/cluster", nil, &summary); err != nil {
		return errorResult(err), clusterOutput{}, nil
	}

	out := clusterOutput{
		Reachable: summary.Reachable, KubernetesVer: summary.KubernetesVer,
		ReadyServers: summary.ReadyNodes, TotalServers: len(summary.Nodes),
		HighAvailability:  summary.HighAvailability,
		CPUUsedPercent:    percent(summary.UsedCPUM, summary.TotalCPUM),
		MemoryUsedPercent: percent(summary.UsedMemoryMB, summary.TotalMemoryMB),
		Servers:           summary.Nodes,
	}
	return textResult(fmt.Sprintf("%d of %d servers ready, %d%% of memory in use.",
		out.ReadyServers, out.TotalServers, out.MemoryUsedPercent)), out, nil
}

func (s *Server) checkReadiness(ctx context.Context, _ *mcp.CallToolRequest, in appIDInput) (*mcp.CallToolResult, readinessOutput, error) {
	var response struct {
		Items []struct {
			Severity string `json:"severity"`
			Title    string `json:"title"`
			Detail   string `json:"detail"`
			Fix      string `json:"fix"`
		} `json:"items"`
	}
	if err := s.client.Do(ctx, "GET", "/api/apps/"+in.AppID+"/scaling/readiness", nil, &response); err != nil {
		return errorResult(err), readinessOutput{}, nil
	}

	out := readinessOutput{Findings: response.Items}
	errors := 0
	for _, finding := range response.Items {
		if finding.Severity == "error" {
			errors++
		}
	}
	switch {
	case errors > 0:
		out.Summary = fmt.Sprintf("%d thing(s) would break if this app ran more than one instance. Fix them before scaling.", errors)
	case len(response.Items) > 0:
		out.Summary = "Nothing would break, but there are suggestions worth acting on."
	default:
		out.Summary = "This app looks safe to scale."
	}
	return textResult(out.Summary), out, nil
}

// --- helpers ---

// teamID works out which team to act on, once.
//
// A token created for the CLI or an assistant usually has no team stored
// alongside it — SKIFITY_URL and SKIFITY_TOKEN are the whole setup — and
// without this every tool that needs a team answered "this token is not tied
// to a team", which is every tool that does anything.
func (s *Server) teamID(ctx context.Context) (string, error) {
	s.teamOnce.Do(func() {
		s.team, s.teamErr = cli.ResolveTeam(ctx, s.client, s.config)
	})
	return s.team, s.teamErr
}

func (s *Server) defaultEnvironment(ctx context.Context) (string, error) {
	teamID, err := s.teamID(ctx)
	if err != nil {
		return "", err
	}
	var projects struct {
		Items []store.Project `json:"items"`
	}
	if err := s.client.Do(ctx, "GET", "/api/teams/"+teamID+"/projects", nil, &projects); err != nil {
		return "", err
	}
	if len(projects.Items) == 0 {
		return "", errdoc.BadRequest("This team has no projects yet.")
	}
	var environments struct {
		Items []store.Environment `json:"items"`
	}
	if err := s.client.Do(ctx, "GET",
		"/api/projects/"+projects.Items[0].ID+"/environments", nil, &environments); err != nil {
		return "", err
	}
	for _, env := range environments.Items {
		if env.Slug == "production" {
			return env.ID, nil
		}
	}
	if len(environments.Items) > 0 {
		return environments.Items[0].ID, nil
	}
	return "", errdoc.BadRequest("That project has no environments.")
}

// previousDeployment finds the version before the one running now.
func (s *Server) previousDeployment(ctx context.Context, appID string) (string, error) {
	var response struct {
		Items []store.Deployment `json:"items"`
	}
	if err := s.client.Do(ctx, "GET", "/api/apps/"+appID+"/deployments?limit=20", nil, &response); err != nil {
		return "", err
	}
	seen := 0
	for _, deployment := range response.Items {
		if deployment.Status != store.DeploySucceeded {
			continue
		}
		seen++
		if seen == 2 {
			return deployment.ID, nil
		}
	}
	return "", errdoc.BadRequest("There is no earlier successful deployment to go back to.")
}

// errorResult turns a Problem into a tool error the assistant can act on.
//
// The cause, impact and fix all go into the text, because an assistant that
// only sees "request failed" will guess, and guessing about someone's
// production cluster is the worst possible outcome.
func errorResult(err error) *mcp.CallToolResult {
	problem := errdoc.From(err)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", problem.Title)
	if problem.Cause != "" {
		fmt.Fprintf(&b, "What happened: %s\n", problem.Cause)
	}
	if problem.Impact != "" {
		fmt.Fprintf(&b, "What it means: %s\n", problem.Impact)
	}
	if problem.Fix != "" {
		fmt.Fprintf(&b, "How to fix it: %s\n", problem.Fix)
	}
	if len(problem.Context) > 0 {
		encoded, _ := json.MarshalIndent(problem.Context, "", "  ")
		fmt.Fprintf(&b, "\nDetails:\n%s\n", encoded)
	}
	fmt.Fprintf(&b, "\nError code: %s", problem.Code)

	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: b.String()}},
	}
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func percent(used, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(used * 100 / total)
}
