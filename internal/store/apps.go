package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const appColumns = `id, environment_id, name, slug, source_type, COALESCE(git_source_id,''), repo_url, branch,
	root_dir, builder, dockerfile_path, image, port, health_path, start_command, replicas,
	autoscale, min_replicas, max_replicas, cpu_target, memory_target, scale_to_zero,
	cpu_request_m, cpu_limit_m, mem_request_mb, mem_limit_mb, auto_deploy, preview_deploys,
	status, created_at, updated_at`

func scanApp(row interface{ Scan(...any) error }) (App, error) {
	var a App
	var created, updated string
	err := row.Scan(&a.ID, &a.EnvironmentID, &a.Name, &a.Slug, &a.SourceType, &a.GitSourceID, &a.RepoURL,
		&a.Branch, &a.RootDir, &a.Builder, &a.DockerfilePath, &a.Image, &a.Port, &a.HealthPath,
		&a.StartCommand, &a.Replicas, &a.Autoscale, &a.MinReplicas, &a.MaxReplicas, &a.CPUTarget,
		&a.MemoryTarget, &a.ScaleToZero, &a.CPURequestM, &a.CPULimitM, &a.MemRequestMB, &a.MemLimitMB,
		&a.AutoDeploy, &a.PreviewDeploys, &a.Status, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return a, ErrNotFound
		}
		return a, fmt.Errorf("scan app: %w", err)
	}
	a.CreatedAt, _ = ParseTime(created)
	a.UpdatedAt, _ = ParseTime(updated)
	return a, nil
}

// CreateApp inserts an app.
func (db *DB) CreateApp(ctx context.Context, a *App) error {
	if a.ID == "" {
		a.ID = NewID("app")
	}
	now := Now()
	_, err := db.Exec(ctx, `INSERT INTO apps
		(id, environment_id, name, slug, source_type, git_source_id, repo_url, branch, root_dir, builder,
		 dockerfile_path, image, port, health_path, start_command, replicas, autoscale, min_replicas,
		 max_replicas, cpu_target, memory_target, scale_to_zero, cpu_request_m, cpu_limit_m,
		 mem_request_mb, mem_limit_mb, auto_deploy, preview_deploys, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.EnvironmentID, a.Name, a.Slug, defaultStr(a.SourceType, "git"), NullString(a.GitSourceID),
		a.RepoURL, a.Branch, a.RootDir, defaultStr(a.Builder, "auto"), a.DockerfilePath, a.Image,
		a.Port, a.HealthPath, a.StartCommand, a.Replicas, a.Autoscale, a.MinReplicas, a.MaxReplicas,
		a.CPUTarget, a.MemoryTarget, a.ScaleToZero, a.CPURequestM, a.CPULimitM, a.MemRequestMB,
		a.MemLimitMB, a.AutoDeploy, a.PreviewDeploys, defaultStr(a.Status, "created"), now, now)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return fmt.Errorf("%w: this environment already has an app named %s", ErrConflict, a.Name)
		}
		return fmt.Errorf("create app: %w", err)
	}
	a.CreatedAt, _ = ParseTime(now)
	a.UpdatedAt = a.CreatedAt
	return nil
}

// GetApp looks an app up by id.
func (db *DB) GetApp(ctx context.Context, id string) (App, error) {
	return scanApp(db.QueryRowContext(ctx, `SELECT `+appColumns+` FROM apps WHERE id = ?`, id))
}

// ListApps returns the apps in an environment.
func (db *DB) ListApps(ctx context.Context, envID string) ([]App, error) {
	return db.queryApps(ctx, `SELECT `+appColumns+` FROM apps WHERE environment_id = ? ORDER BY created_at`, envID)
}

// ListAppsForProject returns every app across a project's environments.
func (db *DB) ListAppsForProject(ctx context.Context, projectID string) ([]App, error) {
	return db.queryApps(ctx, `SELECT `+prefixColumns("a", appColumns)+` FROM apps a
		JOIN environments e ON e.id = a.environment_id
		WHERE e.project_id = ? ORDER BY a.created_at`, projectID)
}

// ListAppsByRepo finds every app built from a repository, which is how a webhook
// knows what to deploy.
func (db *DB) ListAppsByRepo(ctx context.Context, repoURL string) ([]App, error) {
	return db.queryApps(ctx, `SELECT `+appColumns+` FROM apps WHERE repo_url = ? ORDER BY created_at`, repoURL)
}

func (db *DB) queryApps(ctx context.Context, query string, args ...any) ([]App, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateApp writes an app's mutable fields.
func (db *DB) UpdateApp(ctx context.Context, a *App) error {
	now := Now()
	res, err := db.Exec(ctx, `UPDATE apps SET
		name=?, slug=?, source_type=?, git_source_id=?, repo_url=?, branch=?, root_dir=?, builder=?,
		dockerfile_path=?, image=?, port=?, health_path=?, start_command=?, replicas=?, autoscale=?,
		min_replicas=?, max_replicas=?, cpu_target=?, memory_target=?, scale_to_zero=?, cpu_request_m=?,
		cpu_limit_m=?, mem_request_mb=?, mem_limit_mb=?, auto_deploy=?, preview_deploys=?, status=?, updated_at=?
		WHERE id=?`,
		a.Name, a.Slug, a.SourceType, NullString(a.GitSourceID), a.RepoURL, a.Branch, a.RootDir, a.Builder,
		a.DockerfilePath, a.Image, a.Port, a.HealthPath, a.StartCommand, a.Replicas, a.Autoscale,
		a.MinReplicas, a.MaxReplicas, a.CPUTarget, a.MemoryTarget, a.ScaleToZero, a.CPURequestM,
		a.CPULimitM, a.MemRequestMB, a.MemLimitMB, a.AutoDeploy, a.PreviewDeploys, a.Status, now, a.ID)
	if err != nil {
		return fmt.Errorf("update app: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	a.UpdatedAt, _ = ParseTime(now)
	return nil
}

// SetAppStatus updates just the status.
func (db *DB) SetAppStatus(ctx context.Context, id, status string) error {
	_, err := db.Exec(ctx, `UPDATE apps SET status = ?, updated_at = ? WHERE id = ?`, status, Now(), id)
	if err != nil {
		return fmt.Errorf("set app status: %w", err)
	}
	return nil
}

// DeleteApp removes an app and everything attached to it.
func (db *DB) DeleteApp(ctx context.Context, id string) error {
	res, err := db.Exec(ctx, `DELETE FROM apps WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete app: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- variables ---

// SetVariable creates or replaces one variable. value must already be sealed.
func (db *DB) SetVariable(ctx context.Context, v *Variable, sealed string) error {
	if v.ID == "" {
		v.ID = NewID("var")
	}
	now := Now()
	_, err := db.Exec(ctx, `INSERT INTO app_variables (id, app_id, key, value_enc, is_secret, build_time, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT (app_id, key) DO UPDATE SET value_enc = excluded.value_enc,
			is_secret = excluded.is_secret, build_time = excluded.build_time, updated_at = excluded.updated_at`,
		v.ID, v.AppID, v.Key, sealed, v.IsSecret, v.BuildTime, now, now)
	if err != nil {
		return fmt.Errorf("set variable: %w", err)
	}
	v.UpdatedAt, _ = ParseTime(now)
	return nil
}

// variableRow carries the sealed value alongside the model, because callers
// decide whether to decrypt.
type variableRow struct {
	Variable
	Sealed string
}

// ListVariables returns an app's variables with their sealed values.
func (db *DB) ListVariables(ctx context.Context, appID string) ([]variableRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, app_id, key, value_enc, is_secret, build_time, created_at, updated_at
		FROM app_variables WHERE app_id = ? ORDER BY key`, appID)
	if err != nil {
		return nil, fmt.Errorf("list variables: %w", err)
	}
	defer rows.Close()
	out := []variableRow{}
	for rows.Next() {
		var r variableRow
		var created, updated string
		if err := rows.Scan(&r.ID, &r.AppID, &r.Key, &r.Sealed, &r.IsSecret, &r.BuildTime, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan variable: %w", err)
		}
		r.CreatedAt, _ = ParseTime(created)
		r.UpdatedAt, _ = ParseTime(updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteVariable removes one variable.
func (db *DB) DeleteVariable(ctx context.Context, appID, key string) error {
	res, err := db.Exec(ctx, `DELETE FROM app_variables WHERE app_id = ? AND key = ?`, appID, key)
	if err != nil {
		return fmt.Errorf("delete variable: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSharedVariable creates or replaces a project-wide variable.
func (db *DB) SetSharedVariable(ctx context.Context, v *SharedVariable, sealed string) error {
	if v.ID == "" {
		v.ID = NewID("svar")
	}
	now := Now()
	_, err := db.Exec(ctx, `INSERT INTO shared_variables (id, project_id, key, value_enc, is_secret, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (project_id, key) DO UPDATE SET value_enc = excluded.value_enc,
			is_secret = excluded.is_secret, updated_at = excluded.updated_at`,
		v.ID, v.ProjectID, v.Key, sealed, v.IsSecret, now, now)
	if err != nil {
		return fmt.Errorf("set shared variable: %w", err)
	}
	return nil
}

type sharedVariableRow struct {
	SharedVariable
	Sealed string
}

// ListSharedVariables returns a project's shared variables.
func (db *DB) ListSharedVariables(ctx context.Context, projectID string) ([]sharedVariableRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, project_id, key, value_enc, is_secret, created_at, updated_at
		FROM shared_variables WHERE project_id = ? ORDER BY key`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list shared variables: %w", err)
	}
	defer rows.Close()
	out := []sharedVariableRow{}
	for rows.Next() {
		var r sharedVariableRow
		var created, updated string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Key, &r.Sealed, &r.IsSecret, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan shared variable: %w", err)
		}
		r.CreatedAt, _ = ParseTime(created)
		r.UpdatedAt, _ = ParseTime(updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteSharedVariable removes one project-wide variable.
func (db *DB) DeleteSharedVariable(ctx context.Context, projectID, key string) error {
	res, err := db.Exec(ctx, `DELETE FROM shared_variables WHERE project_id = ? AND key = ?`, projectID, key)
	if err != nil {
		return fmt.Errorf("delete shared variable: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSealedSecrets returns every sealed value in the database with the context
// it was sealed under. Master key rotation walks this list.
func (db *DB) ListSealedSecrets(ctx context.Context) ([]SealedRef, error) {
	queries := []struct{ table, idCol, valueCol, ctxPrefix string }{
		{"app_variables", "id", "value_enc", "variable"},
		{"shared_variables", "id", "value_enc", "shared_variable"},
		{"servers", "id", "ssh_key_enc", "server_key"},
		{"databases", "id", "credentials_enc", "database_credentials"},
		{"git_sources", "id", "config_enc", "git_source"},
		{"notification_channels", "id", "config_enc", "notification_channel"},
		{"users", "id", "totp_secret_enc", "totp"},
	}
	out := []SealedRef{}
	for _, q := range queries {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(
			`SELECT %s, %s FROM %s WHERE %s != ''`, q.idCol, q.valueCol, q.table, q.valueCol))
		if err != nil {
			return nil, fmt.Errorf("list sealed values in %s: %w", q.table, err)
		}
		for rows.Next() {
			var ref SealedRef
			if err := rows.Scan(&ref.ID, &ref.Sealed); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan sealed value in %s: %w", q.table, err)
			}
			ref.Table = q.table
			ref.Column = q.valueCol
			ref.ContextPrefix = q.ctxPrefix
			out = append(out, ref)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	// settings rows are keyed by name rather than id.
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM settings WHERE encrypted = 1 AND value != ''`)
	if err != nil {
		return nil, fmt.Errorf("list sealed settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref SealedRef
		if err := rows.Scan(&ref.ID, &ref.Sealed); err != nil {
			return nil, fmt.Errorf("scan sealed setting: %w", err)
		}
		ref.Table = "settings"
		ref.Column = "value"
		ref.KeyColumn = "key"
		ref.ContextPrefix = "setting"
		out = append(out, ref)
	}
	return out, rows.Err()
}

// SealedRef points at one encrypted column value, for rotation.
type SealedRef struct {
	Table         string
	Column        string
	KeyColumn     string // defaults to "id"
	ID            string
	Sealed        string
	ContextPrefix string
}

// UpdateSealed writes a rewrapped envelope back where it came from.
func (db *DB) UpdateSealed(ctx context.Context, ref SealedRef, sealed string) error {
	keyCol := ref.KeyColumn
	if keyCol == "" {
		keyCol = "id"
	}
	// Table and column names come from the fixed list in ListSealedSecrets, never
	// from user input, so interpolating them is safe here.
	_, err := db.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, ref.Table, ref.Column, keyCol),
		sealed, ref.ID)
	if err != nil {
		return fmt.Errorf("rewrap %s.%s: %w", ref.Table, ref.Column, err)
	}
	return nil
}

// --- domains ---

// CreateDomain attaches a hostname to an app.
func (db *DB) CreateDomain(ctx context.Context, d *Domain) error {
	if d.ID == "" {
		d.ID = NewID("dom")
	}
	now := Now()
	_, err := db.Exec(ctx, `INSERT INTO domains (id, app_id, hostname, path, tls, auto, status, status_detail, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		d.ID, d.AppID, d.Hostname, defaultStr(d.Path, "/"), d.TLS, d.Auto,
		defaultStr(d.Status, "pending"), d.StatusDetail, now)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return fmt.Errorf("%w: %s is already used by another app", ErrConflict, d.Hostname)
		}
		return fmt.Errorf("create domain: %w", err)
	}
	d.CreatedAt, _ = ParseTime(now)
	return nil
}

// ListDomains returns an app's domains.
func (db *DB) ListDomains(ctx context.Context, appID string) ([]Domain, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, app_id, hostname, path, tls, auto, status, status_detail, created_at
		FROM domains WHERE app_id = ? ORDER BY auto DESC, hostname`, appID)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		var created string
		if err := rows.Scan(&d.ID, &d.AppID, &d.Hostname, &d.Path, &d.TLS, &d.Auto, &d.Status, &d.StatusDetail, &created); err != nil {
			return nil, fmt.Errorf("scan domain: %w", err)
		}
		d.CreatedAt, _ = ParseTime(created)
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDomainStatus records certificate and routing progress.
func (db *DB) SetDomainStatus(ctx context.Context, id, status, detail string) error {
	_, err := db.Exec(ctx, `UPDATE domains SET status = ?, status_detail = ? WHERE id = ?`, status, detail, id)
	if err != nil {
		return fmt.Errorf("set domain status: %w", err)
	}
	return nil
}

// DeleteDomain detaches a hostname.
func (db *DB) DeleteDomain(ctx context.Context, id string) error {
	res, err := db.Exec(ctx, `DELETE FROM domains WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- volumes ---

// CreateVolume attaches persistent storage to an app.
func (db *DB) CreateVolume(ctx context.Context, v *Volume) error {
	if v.ID == "" {
		v.ID = NewID("vol")
	}
	now := Now()
	_, err := db.Exec(ctx, `INSERT INTO volumes (id, app_id, name, mount_path, size_gb, storage_class, created_at)
		VALUES (?,?,?,?,?,?,?)`, v.ID, v.AppID, v.Name, v.MountPath, v.SizeGB, v.StorageClass, now)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return fmt.Errorf("%w: this app already has a volume named %s", ErrConflict, v.Name)
		}
		return fmt.Errorf("create volume: %w", err)
	}
	v.CreatedAt, _ = ParseTime(now)
	return nil
}

// ListVolumes returns an app's volumes.
func (db *DB) ListVolumes(ctx context.Context, appID string) ([]Volume, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, app_id, name, mount_path, size_gb, storage_class, created_at
		FROM volumes WHERE app_id = ? ORDER BY name`, appID)
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	defer rows.Close()
	out := []Volume{}
	for rows.Next() {
		var v Volume
		var created string
		if err := rows.Scan(&v.ID, &v.AppID, &v.Name, &v.MountPath, &v.SizeGB, &v.StorageClass, &created); err != nil {
			return nil, fmt.Errorf("scan volume: %w", err)
		}
		v.CreatedAt, _ = ParseTime(created)
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteVolume detaches a volume record. The PVC is removed separately so the
// data can be kept deliberately.
func (db *DB) DeleteVolume(ctx context.Context, id string) error {
	res, err := db.Exec(ctx, `DELETE FROM volumes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete volume: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeployedApp is an app together with where it runs and who owns it, which is
// what a watcher needs to compare the panel's picture with the cluster's.
type DeployedApp struct {
	App
	TeamID    string
	Namespace string
}

// ListDeployedApps returns every app that has been deployed at least once,
// across every team.
//
// An app that was created and never deployed has nothing in the cluster to
// compare against, so including it would only produce false alarms.
func (db *DB) ListDeployedApps(ctx context.Context) ([]DeployedApp, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+prefixColumns("a", appColumns)+`, p.team_id, e.namespace
		FROM apps a
		JOIN environments e ON e.id = a.environment_id
		JOIN projects p ON p.id = e.project_id
		WHERE a.status <> 'created'
		ORDER BY a.created_at`)
	if err != nil {
		return nil, fmt.Errorf("list deployed apps: %w", err)
	}
	defer rows.Close()

	out := []DeployedApp{}
	for rows.Next() {
		var a App
		var created, updated string
		var item DeployedApp
		if err := rows.Scan(&a.ID, &a.EnvironmentID, &a.Name, &a.Slug, &a.SourceType, &a.GitSourceID,
			&a.RepoURL, &a.Branch, &a.RootDir, &a.Builder, &a.DockerfilePath, &a.Image, &a.Port,
			&a.HealthPath, &a.StartCommand, &a.Replicas, &a.Autoscale, &a.MinReplicas, &a.MaxReplicas,
			&a.CPUTarget, &a.MemoryTarget, &a.ScaleToZero, &a.CPURequestM, &a.CPULimitM,
			&a.MemRequestMB, &a.MemLimitMB, &a.AutoDeploy, &a.PreviewDeploys, &a.Status,
			&created, &updated, &item.TeamID, &item.Namespace); err != nil {
			return nil, fmt.Errorf("scan deployed app: %w", err)
		}
		a.CreatedAt, _ = ParseTime(created)
		a.UpdatedAt, _ = ParseTime(updated)
		item.App = a
		out = append(out, item)
	}
	return out, rows.Err()
}
