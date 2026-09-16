package deploy

import (
	"context"
	"fmt"
	"strings"

	"skifity/internal/api"
	"skifity/internal/store"
)

// The scaling readiness checker.
//
// Most apps that break when scaled break for one of a handful of reasons, and
// every one of them is visible in the app's own configuration. Saying so before
// the user turns on autoscaling is worth more than a good error afterwards,
// because the failure mode is "some requests behave differently", which is
// almost impossible to debug from the outside.

// ScalingReadiness looks for patterns that break with more than one instance.
func (d *Deployer) ScalingReadiness(ctx context.Context, appID string) ([]api.ScalingFinding, error) {
	app, err := d.db.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	findings := []api.ScalingFinding{}

	// A ReadWriteOnce volume can only be mounted by one node at a time, so a
	// second instance either shares the disk or cannot start at all.
	volumes, err := d.db.ListVolumes(ctx, appID)
	if err != nil {
		return nil, err
	}
	if len(volumes) > 0 {
		paths := make([]string, 0, len(volumes))
		for _, v := range volumes {
			paths = append(paths, v.MountPath)
		}
		findings = append(findings, api.ScalingFinding{
			Code:     "volume",
			Severity: "error",
			Title:    "This app writes to a disk that only one instance can use",
			Detail: fmt.Sprintf(
				"A volume is mounted at %s. That storage can be attached to one server at a time, so a second instance would either fail to start or end up on the same server.",
				strings.Join(paths, ", ")),
			Fix: "Move uploads to object storage such as S3 or MinIO, or turn on cross-node storage in Settings so the volume can be shared. Until then, keep this app at one instance.",
		})
	}

	// Configuration that names a local file or an in-memory store.
	rows, err := d.db.ListVariables(ctx, appID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		finding, found := inspectVariable(row.Key, row.IsSecret, func() string {
			if row.IsSecret {
				// A secret's value is never read for a heuristic; the key name
				// is enough to spot the pattern.
				return ""
			}
			plaintext, err := d.keyring.Open(row.Sealed, "variable:"+appID+":"+row.Key)
			if err != nil {
				return ""
			}
			return string(plaintext)
		})
		if found {
			findings = append(findings, finding)
		}
	}

	// No health path means Kubernetes cannot tell a starting instance from a
	// ready one, so a rollout sends traffic to an instance that is not up yet.
	if app.HealthPath == "" && app.Port > 0 {
		findings = append(findings, api.ScalingFinding{
			Code:     "no_health_check",
			Severity: "warning",
			Title:    "There is no health check path",
			Detail:   "Without one, Skifity can only check that the port is open, which happens before most frameworks are ready to serve.",
			Fix:      "Add a path such as /healthz that returns 200 once the app is ready, and set it under the app's settings.",
		})
	}

	// A single instance with no autoscaling has no redundancy at all.
	if app.Replicas == 1 && !app.Autoscale {
		findings = append(findings, api.ScalingFinding{
			Code:     "single_instance",
			Severity: "info",
			Title:    "This app runs as a single instance",
			Detail:   "If the server it is on restarts, the app is unavailable until Kubernetes starts it somewhere else.",
			Fix:      "Raise the number of instances to two, or turn on autoscaling, once the points above are addressed.",
		})
	}

	// Asking for more than one instance with a volume is the combination that
	// actually breaks, so it is called out separately.
	if (app.Replicas > 1 || app.Autoscale) && len(volumes) > 0 {
		findings = append(findings, api.ScalingFinding{
			Code:     "volume_with_replicas",
			Severity: "error",
			Title:    "More than one instance is configured, and this app has a volume",
			Detail:   "The extra instances will stay pending, or will be scheduled onto the same server and write to the same files at once.",
			Fix:      "Set the instance count back to one, or enable cross-node storage, or move the data out of the volume.",
		})
	}

	return findings, nil
}

// inspectVariable spots configuration that will not survive being scaled.
//
// readValue is a function rather than a value so that a secret's plaintext is
// never read when only the key name is needed.
func inspectVariable(key string, isSecret bool, readValue func() string) (api.ScalingFinding, bool) {
	upper := strings.ToUpper(key)
	value := strings.ToLower(readValue())

	switch {
	case strings.Contains(upper, "SESSION") && (strings.Contains(value, "memory") || value == "file" || strings.Contains(value, "filesystem")):
		return api.ScalingFinding{
			Code:     "in_memory_sessions",
			Severity: "error",
			Title:    "Sessions are kept in memory or on disk",
			Detail:   fmt.Sprintf("%s is set to %q. Each instance would keep its own sessions, so users would be signed out at random as requests land on different instances.", key, readValue()),
			Fix:      "Create a Redis database in this environment and point the session store at it. Skifity links it in as a variable for you.",
		}, true

	case strings.Contains(upper, "CACHE") && (value == "file" || strings.Contains(value, "filesystem")):
		return api.ScalingFinding{
			Code:     "file_cache",
			Severity: "warning",
			Title:    "The cache is stored on local disk",
			Detail:   fmt.Sprintf("%s is set to %q, so each instance would have its own cache and they would disagree.", key, readValue()),
			Fix:      "Use Redis for the cache, or accept that cached values differ between instances.",
		}, true

	case strings.Contains(value, "sqlite") || strings.HasSuffix(value, ".db") || strings.HasSuffix(value, ".sqlite3"):
		return api.ScalingFinding{
			Code:     "sqlite",
			Severity: "error",
			Title:    "This app uses SQLite",
			Detail:   fmt.Sprintf("%s points at a SQLite file. SQLite is a single file on one disk, so instances on different servers cannot share it.", key),
			Fix:      "Create a PostgreSQL database in this environment and point the app at it. Skifity injects the connection string for you.",
		}, true

	case upper == "UPLOAD_DIR" || upper == "UPLOADS_PATH" || strings.Contains(upper, "STORAGE_PATH"):
		return api.ScalingFinding{
			Code:     "local_uploads",
			Severity: "error",
			Title:    "Uploaded files are written to local disk",
			Detail:   fmt.Sprintf("%s points at a directory inside the container. Files written by one instance would be invisible to the others, and would disappear on the next deploy.", key),
			Fix:      "Store uploads in S3-compatible object storage. MinIO is available as a one-click template if you would rather not use a cloud provider.",
		}, true

	case strings.Contains(upper, "CRON") && value == "true":
		return api.ScalingFinding{
			Code:     "in_process_cron",
			Severity: "warning",
			Title:    "Scheduled jobs run inside the app",
			Detail:   fmt.Sprintf("%s is on. With several instances, every instance runs the schedule, so each job runs several times.", key),
			Fix:      "Run the scheduler as a separate app with one instance, or use a lock so only one instance runs each job.",
		}, true
	}
	return api.ScalingFinding{}, false
}

// AppsNeedingAttention lists apps whose live state does not match what was
// asked for, which is what the dashboard's warning badge is built from.
func (d *Deployer) AppsNeedingAttention(ctx context.Context, envID string) ([]store.App, error) {
	apps, err := d.db.ListApps(ctx, envID)
	if err != nil {
		return nil, err
	}
	var out []store.App
	for _, app := range apps {
		if app.Status == "failed" || app.Status == "crashing" {
			out = append(out, app)
		}
	}
	return out, nil
}
