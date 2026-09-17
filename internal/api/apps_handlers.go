package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"skifity/internal/errdoc"
	"skifity/internal/events"
	"skifity/internal/gitsrc"
	"skifity/internal/kube"
	"skifity/internal/store"
)

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	env, _, err := s.authorizeEnvironment(r, chi.URLParam(r, "envID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	apps, err := s.db.ListApps(r.Context(), env.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeList(w, apps)
}

type createAppRequest struct {
	Name           string `json:"name"`
	SourceType     string `json:"source_type,omitempty"`
	GitSourceID    string `json:"git_source_id,omitempty"`
	RepoURL        string `json:"repo_url,omitempty"`
	Branch         string `json:"branch,omitempty"`
	RootDir        string `json:"root_dir,omitempty"`
	Builder        string `json:"builder,omitempty"`
	DockerfilePath string `json:"dockerfile_path,omitempty"`
	Image          string `json:"image,omitempty"`
	Port           int    `json:"port,omitempty"`
	HealthPath     string `json:"health_path,omitempty"`
	StartCommand   string `json:"start_command,omitempty"`
	ReleaseCommand string `json:"release_command,omitempty"`
	Deploy         bool   `json:"deploy,omitempty"`
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	env, user, err := s.authorizeEnvironment(r, chi.URLParam(r, "envID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req createAppRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, r, errdoc.BadRequest("An app needs a name."))
		return
	}
	sourceType := req.SourceType
	if sourceType == "" {
		if req.Image != "" {
			sourceType = "image"
		} else {
			sourceType = "git"
		}
	}
	repoURL := strings.TrimSpace(req.RepoURL)
	switch sourceType {
	case "git":
		if repoURL == "" {
			writeError(w, r, errdoc.BadRequest("Enter the URL of the Git repository to deploy."))
			return
		}
		// The address ends up in a shell script in the build pod and decides
		// where a Git token is sent, so it is checked here rather than trusted
		// there.
		checked, err := gitsrc.ValidateRepoURL(repoURL)
		if err != nil {
			writeError(w, r, errdoc.BadRequest(capitalise(err.Error())+"."))
			return
		}
		repoURL = checked
	case "image":
		if strings.TrimSpace(req.Image) == "" {
			writeError(w, r, errdoc.BadRequest("Enter the image to run, for example nginx:1.27."))
			return
		}
	case "compose":
	default:
		writeError(w, r, errdoc.BadRequest("Source must be git, image or compose."))
		return
	}

	// An app and a database in one environment share a namespace, and both
	// render a Service under their slug. Two of them under one name is not two
	// things side by side: the second takes the first's Service over.
	slug := kube.Slugify(name)
	if owner, err := s.db.SlugOwnerInEnvironment(r.Context(), env.ID, slug); err != nil {
		writeError(w, r, err)
		return
	} else if owner != "" {
		writeError(w, r, errdoc.NameTaken(owner, name))
		return
	}

	app := store.App{
		EnvironmentID:  env.ID,
		Name:           name,
		Slug:           slug,
		SourceType:     sourceType,
		GitSourceID:    req.GitSourceID,
		RepoURL:        repoURL,
		Branch:         strings.TrimSpace(req.Branch),
		RootDir:        strings.TrimPrefix(strings.TrimSpace(req.RootDir), "/"),
		Builder:        defaultString(req.Builder, "auto"),
		DockerfilePath: strings.TrimSpace(req.DockerfilePath),
		Image:          strings.TrimSpace(req.Image),
		Port:           defaultInt(req.Port, kube.DefaultAppPort),
		HealthPath:     strings.TrimSpace(req.HealthPath),
		StartCommand:   strings.TrimSpace(req.StartCommand),
		ReleaseCommand: strings.TrimSpace(req.ReleaseCommand),
		// Safe defaults, per the product principles: one instance, modest
		// limits, health checks on, deploy on push.
		Replicas:     1,
		MinReplicas:  1,
		MaxReplicas:  3,
		CPUTarget:    75,
		CPURequestM:  50,
		CPULimitM:    1000,
		MemRequestMB: 128,
		MemLimitMB:   512,
		AutoDeploy:   true,
		Status:       "created",
	}
	if err := s.db.CreateApp(r.Context(), &app); err != nil {
		writeError(w, r, err)
		return
	}

	teamID, _ := s.db.TeamIDForEnvironment(r.Context(), env.ID)
	s.audit(r, teamID, "app.created", "app", app.ID, app.Name)
	s.hub.Publish(events.TeamTopic(teamID), "app.created", app)

	if req.Deploy && s.deployer != nil {
		deployment, err := s.deployer.Deploy(r.Context(), DeployRequest{
			AppID: app.ID, Trigger: "create", CreatedBy: user.ID,
		})
		if err != nil {
			// The app exists; report the deploy failure without losing it.
			s.log.Warn("could not start the first deploy", "app", app.ID, "error", err)
		} else {
			writeJSON(w, http.StatusCreated, map[string]any{"app": app, "deployment": deployment})
			return
		}
	}
	writeJSON(w, http.StatusCreated, app)
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

type updateAppRequest struct {
	Name           *string `json:"name,omitempty"`
	Branch         *string `json:"branch,omitempty"`
	RootDir        *string `json:"root_dir,omitempty"`
	Builder        *string `json:"builder,omitempty"`
	DockerfilePath *string `json:"dockerfile_path,omitempty"`
	Image          *string `json:"image,omitempty"`
	Port           *int    `json:"port,omitempty"`
	HealthPath     *string `json:"health_path,omitempty"`
	StartCommand   *string `json:"start_command,omitempty"`
	ReleaseCommand *string `json:"release_command,omitempty"`
	AutoDeploy     *bool   `json:"auto_deploy,omitempty"`
	PreviewDeploys *bool   `json:"preview_deploys,omitempty"`
	CPURequestM    *int    `json:"cpu_request_m,omitempty"`
	CPULimitM      *int    `json:"cpu_limit_m,omitempty"`
	MemRequestMB   *int    `json:"mem_request_mb,omitempty"`
	MemLimitMB     *int    `json:"mem_limit_mb,omitempty"`
}

func (s *Server) handleUpdateApp(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req updateAppRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}

	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		app.Name = strings.TrimSpace(*req.Name)
	}
	assignString(&app.Branch, req.Branch)
	assignString(&app.RootDir, req.RootDir)
	assignString(&app.Builder, req.Builder)
	assignString(&app.DockerfilePath, req.DockerfilePath)
	assignString(&app.Image, req.Image)
	assignString(&app.HealthPath, req.HealthPath)
	assignString(&app.StartCommand, req.StartCommand)
	assignString(&app.ReleaseCommand, req.ReleaseCommand)
	if req.Port != nil {
		if *req.Port < 0 || *req.Port > 65535 {
			writeError(w, r, errdoc.BadRequest("The port must be between 1 and 65535."))
			return
		}
		app.Port = *req.Port
	}
	if req.AutoDeploy != nil {
		app.AutoDeploy = *req.AutoDeploy
	}
	if req.PreviewDeploys != nil {
		app.PreviewDeploys = *req.PreviewDeploys
	}
	if err := applyResourceChanges(&app, req); err != nil {
		writeError(w, r, err)
		return
	}

	if err := s.db.UpdateApp(r.Context(), &app); err != nil {
		writeError(w, r, err)
		return
	}

	// Runtime changes are applied by a rollout, never a rebuild: this is the
	// promise in ADR-0007, and the fix for the most common complaint about
	// comparable products.
	if s.deployer != nil {
		if err := s.deployer.Sync(r.Context(), app.ID); err != nil {
			s.log.Warn("could not apply app changes to the cluster", "app", app.ID, "error", err)
		}
	}

	teamID, _ := s.db.TeamIDForApp(r.Context(), app.ID)
	s.audit(r, teamID, "app.updated", "app", app.ID, app.Name)
	writeJSON(w, http.StatusOK, app)
}

func applyResourceChanges(app *store.App, req updateAppRequest) error {
	if req.CPURequestM != nil {
		app.CPURequestM = *req.CPURequestM
	}
	if req.CPULimitM != nil {
		app.CPULimitM = *req.CPULimitM
	}
	if req.MemRequestMB != nil {
		app.MemRequestMB = *req.MemRequestMB
	}
	if req.MemLimitMB != nil {
		app.MemLimitMB = *req.MemLimitMB
	}
	if app.CPURequestM < 10 || app.MemRequestMB < 16 {
		return errdoc.BadRequest("Reserve at least 10 millicores of CPU and 16 MB of memory, or the app will not start reliably.")
	}
	// A limit below the request is rejected by Kubernetes with a message nobody
	// can act on, so catch it here with one they can.
	if app.CPULimitM > 0 && app.CPULimitM < app.CPURequestM {
		return errdoc.BadRequest("The CPU limit cannot be lower than the CPU reservation.")
	}
	if app.MemLimitMB > 0 && app.MemLimitMB < app.MemRequestMB {
		return errdoc.BadRequest("The memory limit cannot be lower than the memory reservation.")
	}
	return nil
}

func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	env, err := s.db.GetEnvironment(r.Context(), app.EnvironmentID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if s.cluster != nil {
		if err := s.cluster.DeleteApp(r.Context(), env.Namespace, app.Slug); err != nil {
			s.log.Warn("could not remove app from the cluster", "app", app.ID, "error", err)
		}
	}
	if err := s.db.DeleteApp(r.Context(), app.ID); err != nil {
		writeError(w, r, err)
		return
	}
	teamID, _ := s.db.TeamIDForEnvironment(r.Context(), env.ID)
	s.audit(r, teamID, "app.deleted", "app", app.ID, app.Name)
	s.hub.Publish(events.TeamTopic(teamID), "app.deleted", app)
	writeOK(w)
}

func (s *Server) handleAppStatus(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	env, err := s.db.GetEnvironment(r.Context(), app.EnvironmentID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if s.cluster == nil {
		writeJSON(w, http.StatusOK, AppRuntimeStatus{Phase: "unknown", Detail: "The panel is not connected to a cluster."})
		return
	}
	status, err := s.cluster.AppStatus(r.Context(), env.Namespace, app.Slug)
	if err != nil {
		writeJSON(w, http.StatusOK, AppRuntimeStatus{Phase: "unknown", Detail: err.Error()})
		return
	}
	domains, err := s.db.ListDomains(r.Context(), app.ID)
	if err == nil {
		for _, d := range domains {
			scheme := "http://"
			if d.TLS {
				scheme = "https://"
			}
			status.URLs = append(status.URLs, scheme+d.Hostname)
		}
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleRestartApp(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	env, err := s.db.GetEnvironment(r.Context(), app.EnvironmentID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if s.cluster == nil {
		writeError(w, r, errdoc.ClusterUnreachable(nil))
		return
	}
	if err := s.cluster.RestartApp(r.Context(), env.Namespace, app.Slug); err != nil {
		writeError(w, r, err)
		return
	}
	teamID, _ := s.db.TeamIDForEnvironment(r.Context(), env.ID)
	s.audit(r, teamID, "app.restarted", "app", app.ID, app.Name)
	writeOK(w)
}

func (s *Server) handleAppAdvanced(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	env, err := s.db.GetEnvironment(r.Context(), app.EnvironmentID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if s.cluster == nil {
		writeError(w, r, errdoc.ClusterUnreachable(nil))
		return
	}
	// This is the one place the panel shows Kubernetes objects, so that an
	// advanced user is never blocked by the abstraction.
	manifests, err := s.cluster.Manifests(r.Context(), app, env)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"namespace": env.Namespace,
		"name":      app.Slug,
		"manifests": manifests,
	})
}

// --- variables ---

func (s *Server) handleListVariables(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rows, err := s.db.ListVariables(r.Context(), app.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]store.Variable, 0, len(rows))
	for _, row := range rows {
		v := row.Variable
		if v.IsSecret {
			// A secret is never shown again after it is set. This is the whole
			// point of storing it encrypted.
			v.Value = ""
		} else if plaintext, err := s.keyring.Open(row.Sealed, variableContext(app.ID, v.Key)); err == nil {
			v.Value = string(plaintext)
		}
		out = append(out, v)
	}
	writeList(w, out)
}

type setVariableRequest struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	IsSecret  bool   `json:"is_secret,omitempty"`
	BuildTime bool   `json:"build_time,omitempty"`
}

func (s *Server) handleSetVariable(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req setVariableRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	key, err := kube.SanitiseEnvKey(req.Key)
	if err != nil {
		writeError(w, r, errdoc.BadRequest(err.Error()))
		return
	}

	sealed, err := s.keyring.Seal([]byte(req.Value), variableContext(app.ID, key))
	if err != nil {
		writeError(w, r, err)
		return
	}
	variable := store.Variable{AppID: app.ID, Key: key, IsSecret: req.IsSecret, BuildTime: req.BuildTime}
	if err := s.db.SetVariable(r.Context(), &variable, sealed); err != nil {
		writeError(w, r, err)
		return
	}

	// A build-time variable changes the build fingerprint, so it does rebuild.
	// A runtime variable only rolls out. Either way the user is told which.
	rebuilt := req.BuildTime
	if s.deployer != nil && !rebuilt {
		if err := s.deployer.Sync(r.Context(), app.ID); err != nil {
			s.log.Warn("could not apply variable change", "app", app.ID, "error", err)
		}
	}

	teamID, _ := s.db.TeamIDForApp(r.Context(), app.ID)
	// The value is never audited, only the key.
	s.audit(r, teamID, "variable.set", "app", app.ID, key)
	variable.Value = ""
	writeJSON(w, http.StatusOK, map[string]any{"variable": variable, "requires_rebuild": rebuilt})
}

func (s *Server) handleDeleteVariable(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := chi.URLParam(r, "key")
	if err := s.db.DeleteVariable(r.Context(), app.ID, key); err != nil {
		writeError(w, r, err)
		return
	}
	if s.deployer != nil {
		if err := s.deployer.Sync(r.Context(), app.ID); err != nil {
			s.log.Warn("could not apply variable removal", "app", app.ID, "error", err)
		}
	}
	teamID, _ := s.db.TeamIDForApp(r.Context(), app.ID)
	s.audit(r, teamID, "variable.deleted", "app", app.ID, key)
	writeOK(w)
}

func (s *Server) handleListSharedVariables(w http.ResponseWriter, r *http.Request) {
	project, _, err := s.authorizeProject(r, chi.URLParam(r, "projectID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rows, err := s.db.ListSharedVariables(r.Context(), project.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]store.SharedVariable, 0, len(rows))
	for _, row := range rows {
		v := row.SharedVariable
		if v.IsSecret {
			v.Value = ""
		} else if plaintext, err := s.keyring.Open(row.Sealed, sharedVariableContext(project.ID, v.Key)); err == nil {
			v.Value = string(plaintext)
		}
		out = append(out, v)
	}
	writeList(w, out)
}

func (s *Server) handleSetSharedVariable(w http.ResponseWriter, r *http.Request) {
	project, _, err := s.authorizeProject(r, chi.URLParam(r, "projectID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req setVariableRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	key, err := kube.SanitiseEnvKey(req.Key)
	if err != nil {
		writeError(w, r, errdoc.BadRequest(err.Error()))
		return
	}
	sealed, err := s.keyring.Seal([]byte(req.Value), sharedVariableContext(project.ID, key))
	if err != nil {
		writeError(w, r, err)
		return
	}
	variable := store.SharedVariable{ProjectID: project.ID, Key: key, IsSecret: req.IsSecret}
	if err := s.db.SetSharedVariable(r.Context(), &variable, sealed); err != nil {
		writeError(w, r, err)
		return
	}

	// A shared variable reaches every app in the project, so every app needs a
	// rollout, not just the one being edited.
	if s.deployer != nil {
		apps, err := s.db.ListAppsForProject(r.Context(), project.ID)
		if err == nil {
			for _, app := range apps {
				if err := s.deployer.Sync(r.Context(), app.ID); err != nil {
					s.log.Warn("could not apply shared variable", "app", app.ID, "error", err)
				}
			}
		}
	}
	s.audit(r, project.TeamID, "shared_variable.set", "project", project.ID, key)
	variable.Value = ""
	writeJSON(w, http.StatusOK, variable)
}

func (s *Server) handleDeleteSharedVariable(w http.ResponseWriter, r *http.Request) {
	project, _, err := s.authorizeProject(r, chi.URLParam(r, "projectID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	key := chi.URLParam(r, "key")
	if err := s.db.DeleteSharedVariable(r.Context(), project.ID, key); err != nil {
		writeError(w, r, err)
		return
	}
	s.audit(r, project.TeamID, "shared_variable.deleted", "project", project.ID, key)
	writeOK(w)
}

// --- domains ---

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	domains, err := s.db.ListDomains(r.Context(), app.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeList(w, domains)
}

type addDomainRequest struct {
	Hostname string `json:"hostname"`
	TLS      *bool  `json:"tls,omitempty"`
	Path     string `json:"path,omitempty"`
}

func (s *Server) handleAddDomain(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req addDomainRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	hostname := strings.ToLower(strings.TrimSpace(req.Hostname))
	hostname = strings.TrimPrefix(strings.TrimPrefix(hostname, "https://"), "http://")
	hostname = strings.TrimSuffix(strings.Split(hostname, "/")[0], ".")
	if !kube.ValidHostname(hostname) {
		writeError(w, r, errdoc.BadRequest("That does not look like a domain name. Enter something like app.example.com."))
		return
	}

	tls := true
	if req.TLS != nil {
		tls = *req.TLS
	}
	domain := store.Domain{
		AppID:    app.ID,
		Hostname: hostname,
		Path:     defaultString(req.Path, "/"),
		TLS:      tls,
		Status:   "pending",
	}
	if err := s.db.CreateDomain(r.Context(), &domain); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, r, errdoc.Conflict(
				"The domain "+hostname+" is already attached to another app.",
				"Remove it from the other app first, or pick a different hostname."))
			return
		}
		writeError(w, r, err)
		return
	}
	if s.deployer != nil {
		if err := s.deployer.Sync(r.Context(), app.ID); err != nil {
			s.log.Warn("could not apply new domain", "app", app.ID, "error", err)
		}
	}
	teamID, _ := s.db.TeamIDForApp(r.Context(), app.ID)
	s.audit(r, teamID, "domain.added", "app", app.ID, hostname)
	writeJSON(w, http.StatusCreated, domain)
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	domainID := chi.URLParam(r, "domainID")
	// Confirm the domain belongs to this app rather than trusting the URL.
	domains, err := s.db.ListDomains(r.Context(), app.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	for _, d := range domains {
		if d.ID != domainID {
			continue
		}
		if d.Auto {
			writeError(w, r, errdoc.BadRequest("The automatic domain cannot be removed. It is how the app stays reachable while you set up your own domain."))
			return
		}
		if err := s.db.DeleteDomain(r.Context(), domainID); err != nil {
			writeError(w, r, err)
			return
		}
		if s.deployer != nil {
			if err := s.deployer.Sync(r.Context(), app.ID); err != nil {
				s.log.Warn("could not apply domain removal", "app", app.ID, "error", err)
			}
		}
		teamID, _ := s.db.TeamIDForApp(r.Context(), app.ID)
		s.audit(r, teamID, "domain.removed", "app", app.ID, d.Hostname)
		writeOK(w)
		return
	}
	writeError(w, r, errdoc.NotFound("domain", domainID))
}

// --- volumes ---

func (s *Server) handleListVolumes(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	volumes, err := s.db.ListVolumes(r.Context(), app.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeList(w, volumes)
}

type createVolumeRequest struct {
	Name      string `json:"name"`
	MountPath string `json:"mount_path"`
	SizeGB    int    `json:"size_gb"`
}

func (s *Server) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req createVolumeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if !strings.HasPrefix(req.MountPath, "/") {
		writeError(w, r, errdoc.BadRequest("The mount path must be absolute, for example /data."))
		return
	}
	if req.SizeGB < 1 {
		req.SizeGB = 1
	}
	volume := store.Volume{
		AppID:     app.ID,
		Name:      kube.Slugify(defaultString(req.Name, "data")),
		MountPath: strings.TrimSuffix(req.MountPath, "/"),
		SizeGB:    req.SizeGB,
	}
	if err := s.db.CreateVolume(r.Context(), &volume); err != nil {
		writeError(w, r, err)
		return
	}
	if s.deployer != nil {
		if err := s.deployer.Sync(r.Context(), app.ID); err != nil {
			s.log.Warn("could not attach volume", "app", app.ID, "error", err)
		}
	}
	teamID, _ := s.db.TeamIDForApp(r.Context(), app.ID)
	s.audit(r, teamID, "volume.created", "app", app.ID, volume.Name)
	writeJSON(w, http.StatusCreated, volume)
}

// handleListVolumeBackups lists the copies of one volume.
func (s *Server) handleListVolumeBackups(w http.ResponseWriter, r *http.Request) {
	volume, _, err := s.authorizeVolume(r, store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	backups, err := s.db.ListBackups(r.Context(), "volume", volume.ID, queryInt(r, "limit", 50))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeList(w, backups)
}

// handleCreateVolumeBackup copies a volume to storage now.
func (s *Server) handleCreateVolumeBackup(w http.ResponseWriter, r *http.Request) {
	volume, app, err := s.authorizeVolume(r, store.RoleMember)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if s.backups == nil {
		writeError(w, r, errdoc.NotConfigured("Backups", "Settings, then Storage"))
		return
	}
	backup, err := s.backups.Run(r.Context(), "volume", volume.ID, "manual")
	if err != nil {
		writeError(w, r, err)
		return
	}
	teamID, _ := s.db.TeamIDForApp(r.Context(), app.ID)
	s.audit(r, teamID, "backup.started", "volume", volume.ID, app.Name+" / "+volume.Name)
	writeJSON(w, http.StatusAccepted, backup)
}

// authorizeVolume resolves a volume through its app, so a volume id from
// another team cannot be reached by guessing it.
func (s *Server) authorizeVolume(r *http.Request, required store.Role) (store.Volume, store.App, error) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), required)
	if err != nil {
		return store.Volume{}, store.App{}, err
	}
	volumeID := chi.URLParam(r, "volumeID")
	volume, err := s.db.GetVolume(r.Context(), volumeID)
	if err != nil || volume.AppID != app.ID {
		return store.Volume{}, store.App{}, errdoc.NotFound("volume", volumeID)
	}
	return volume, app, nil
}

func (s *Server) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.authorizeApp(r, chi.URLParam(r, "appID"), store.RoleAdmin)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.db.DeleteVolume(r.Context(), app.ID, chi.URLParam(r, "volumeID")); err != nil {
		writeError(w, r, err)
		return
	}
	teamID, _ := s.db.TeamIDForApp(r.Context(), app.ID)
	s.audit(r, teamID, "volume.deleted", "app", app.ID, chi.URLParam(r, "volumeID"))
	writeOK(w)
}

// variableContext and sharedVariableContext bind a sealed value to exactly one
// row, so a ciphertext copied between rows cannot be decrypted.
func variableContext(appID, key string) string { return "variable:" + appID + ":" + key }
func sharedVariableContext(projectID, key string) string {
	return "shared_variable:" + projectID + ":" + key
}

// defaultInt is for the settings where zero means "nothing was said" rather
// than "zero".
func defaultInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

// capitalise turns a validator's message into a sentence, because validators
// speak in fragments and the error catalogue speaks in sentences.
func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func assignString(dst *string, src *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}
