// Package templates is the one-click app catalogue.
//
// A template is a description of an app, the databases it needs and the
// variables that wire them together. Installing one creates ordinary Skifity
// resources, so there is nothing special about a templated app afterwards: it
// can be scaled, backed up and rolled back like any other.
package templates

import "strings"

// Template is one installable application.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	// Icon is a short identifier the UI maps to an icon; no remote images.
	Icon string `json:"icon"`
	// Website is where to learn what the software does.
	Website string `json:"website"`
	// Beta marks templates that are known to need extra care.
	Beta bool `json:"beta,omitempty"`
	// Services are the containers to run.
	Services []Service `json:"services"`
	// Databases are provisioned first, so their connection strings exist
	// before the services start.
	Databases []DatabaseSpec `json:"databases"`
	// Inputs are asked for at install time, such as an admin email.
	Inputs []Input `json:"inputs,omitempty"`
	// Notes appear after installation, for the steps we cannot automate.
	Notes string `json:"notes,omitempty"`
}

// Service is one container in a template.
type Service struct {
	Name         string            `json:"name"`
	Image        string            `json:"image"`
	Port         int               `json:"port"`
	HealthPath   string            `json:"health_path,omitempty"`
	Variables    map[string]string `json:"variables,omitempty"`
	Volumes      []VolumeSpec      `json:"volumes,omitempty"`
	MemRequestMB int               `json:"mem_request_mb,omitempty"`
	MemLimitMB   int               `json:"mem_limit_mb,omitempty"`
	CPURequestM  int               `json:"cpu_request_m,omitempty"`
	CPULimitM    int               `json:"cpu_limit_m,omitempty"`
	// Public means this service gets a domain; a worker does not.
	Public bool `json:"public"`
}

// VolumeSpec is persistent storage a template needs.
type VolumeSpec struct {
	Name      string `json:"name"`
	MountPath string `json:"mount_path"`
	SizeGB    int    `json:"size_gb"`
}

// DatabaseSpec is a managed database a template needs.
type DatabaseSpec struct {
	Name      string `json:"name"`
	Engine    string `json:"engine"`
	StorageGB int    `json:"storage_gb"`
	// LinkTo names the service that gets the connection string, and VarName is
	// the variable it arrives as.
	LinkTo  string `json:"link_to"`
	VarName string `json:"var_name"`
}

// Input is a value asked for at install time.
type Input struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"`
	Default  string `json:"default,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
	// Generate fills the value with a random secret when left empty, which is
	// what most of these actually want.
	Generate bool `json:"generate,omitempty"`
}

// catalogue is deliberately short. A template that is not kept working is worse
// than no template, so each one here is something we can keep an eye on.
var catalogue = []Template{
	{
		ID: "wordpress", Name: "WordPress", Category: "cms", Icon: "wordpress",
		Description: "The blogging and content platform that runs a large share of the web.",
		Website:     "https://wordpress.org",
		Databases: []DatabaseSpec{
			{Name: "wordpress-db", Engine: "mysql", StorageGB: 5, LinkTo: "wordpress", VarName: "WORDPRESS_DB_URL"},
		},
		Services: []Service{{
			Name: "wordpress", Image: "wordpress:6-apache", Port: 80, Public: true,
			HealthPath:   "/wp-admin/install.php",
			Volumes:      []VolumeSpec{{Name: "content", MountPath: "/var/www/html/wp-content", SizeGB: 5}},
			MemRequestMB: 256, MemLimitMB: 1024, CPURequestM: 100, CPULimitM: 1000,
		}},
		Notes: "Open the site to finish the WordPress installer. The uploads folder is on a volume, so it survives redeploys.",
	},
	{
		ID: "n8n", Name: "n8n", Category: "automation", Icon: "n8n",
		Description: "Workflow automation you host yourself, with hundreds of integrations.",
		Website:     "https://n8n.io",
		Databases: []DatabaseSpec{
			{Name: "n8n-db", Engine: "postgres", StorageGB: 5, LinkTo: "n8n", VarName: "DB_POSTGRESDB_URL"},
		},
		Services: []Service{{
			Name: "n8n", Image: "n8nio/n8n:latest", Port: 5678, Public: true, HealthPath: "/healthz",
			Variables: map[string]string{
				"DB_TYPE":                               "postgresdb",
				"N8N_PROTOCOL":                          "https",
				"GENERIC_TIMEZONE":                      "UTC",
				"N8N_ENFORCE_SETTINGS_FILE_PERMISSIONS": "true",
			},
			Volumes:      []VolumeSpec{{Name: "data", MountPath: "/home/node/.n8n", SizeGB: 2}},
			MemRequestMB: 256, MemLimitMB: 1024, CPURequestM: 100, CPULimitM: 1000,
		}},
		Inputs: []Input{
			{Key: "N8N_ENCRYPTION_KEY", Label: "Encryption key", Secret: true, Generate: true,
				Help: "n8n encrypts stored credentials with this. Losing it means losing them."},
		},
	},
	{
		ID: "ghost", Name: "Ghost", Category: "cms", Icon: "ghost",
		Description: "A fast publishing platform for newsletters and blogs.",
		Website:     "https://ghost.org",
		Databases: []DatabaseSpec{
			{Name: "ghost-db", Engine: "mysql", StorageGB: 5, LinkTo: "ghost", VarName: "DATABASE_URL"},
		},
		Services: []Service{{
			Name: "ghost", Image: "ghost:5-alpine", Port: 2368, Public: true, HealthPath: "/",
			Variables:    map[string]string{"NODE_ENV": "production", "database__client": "mysql"},
			Volumes:      []VolumeSpec{{Name: "content", MountPath: "/var/lib/ghost/content", SizeGB: 5}},
			MemRequestMB: 256, MemLimitMB: 1024, CPURequestM: 100, CPULimitM: 1000,
		}},
		Notes: "Set the mail settings in Ghost's admin area, or it cannot send member emails.",
	},
	{
		ID: "uptime-kuma", Name: "Uptime Kuma", Category: "monitoring", Icon: "uptime-kuma",
		Description: "Watch your sites and services, and get told when one goes down.",
		Website:     "https://uptime.kuma.pet",
		Services: []Service{{
			Name: "uptime-kuma", Image: "louislam/uptime-kuma:1", Port: 3001, Public: true,
			Volumes:      []VolumeSpec{{Name: "data", MountPath: "/app/data", SizeGB: 2}},
			MemRequestMB: 128, MemLimitMB: 512, CPURequestM: 50, CPULimitM: 500,
		}},
		Notes: "Uptime Kuma keeps its own SQLite database on the volume, so it runs as a single instance.",
	},
	{
		ID: "plausible", Name: "Plausible Analytics", Category: "analytics", Icon: "plausible", Beta: true,
		Description: "Privacy-friendly website analytics without cookies.",
		Website:     "https://plausible.io",
		Databases: []DatabaseSpec{
			{Name: "plausible-db", Engine: "postgres", StorageGB: 10, LinkTo: "plausible", VarName: "DATABASE_URL"},
		},
		Services: []Service{{
			Name: "plausible", Image: "ghcr.io/plausible/community-edition:v2", Port: 8000, Public: true,
			Variables:    map[string]string{"DISABLE_REGISTRATION": "invite_only"},
			MemRequestMB: 256, MemLimitMB: 1024, CPURequestM: 100, CPULimitM: 1000,
		}},
		Inputs: []Input{
			{Key: "SECRET_KEY_BASE", Label: "Secret key base", Secret: true, Generate: true},
			{Key: "BASE_URL", Label: "Public URL", Required: true,
				Help: "The address people will reach this at, including https://."},
		},
		Notes: "Plausible also needs ClickHouse for its event data. This template sets up the app and PostgreSQL; add ClickHouse yourself, or use the official Compose file, until the ClickHouse template lands.",
	},
	{
		ID: "vaultwarden", Name: "Vaultwarden", Category: "productivity", Icon: "vaultwarden",
		Description: "A lightweight password manager server compatible with Bitwarden clients.",
		Website:     "https://github.com/dani-garcia/vaultwarden",
		Services: []Service{{
			Name: "vaultwarden", Image: "vaultwarden/server:latest", Port: 80, Public: true, HealthPath: "/alive",
			Variables:    map[string]string{"SIGNUPS_ALLOWED": "false", "WEBSOCKET_ENABLED": "true"},
			Volumes:      []VolumeSpec{{Name: "data", MountPath: "/data", SizeGB: 2}},
			MemRequestMB: 64, MemLimitMB: 256, CPURequestM: 50, CPULimitM: 500,
		}},
		Inputs: []Input{
			{Key: "ADMIN_TOKEN", Label: "Admin token", Secret: true, Generate: true,
				Help: "Needed to reach the admin page at /admin."},
		},
		Notes: "Sign-ups are turned off. Use the admin page to invite the first user, then keep them off.",
	},
	{
		ID: "umami", Name: "Umami", Category: "analytics", Icon: "umami",
		Description: "Simple, self-hosted website analytics.",
		Website:     "https://umami.is",
		Databases: []DatabaseSpec{
			{Name: "umami-db", Engine: "postgres", StorageGB: 5, LinkTo: "umami", VarName: "DATABASE_URL"},
		},
		Services: []Service{{
			Name: "umami", Image: "ghcr.io/umami-software/umami:postgresql-latest", Port: 3000,
			Public: true, HealthPath: "/api/heartbeat",
			Variables:    map[string]string{"DATABASE_TYPE": "postgresql"},
			MemRequestMB: 128, MemLimitMB: 512, CPURequestM: 50, CPULimitM: 500,
		}},
		Inputs: []Input{{Key: "APP_SECRET", Label: "App secret", Secret: true, Generate: true}},
		Notes:  "The first sign-in is admin / umami. Change it immediately.",
	},
	{
		ID: "minio", Name: "MinIO", Category: "storage", Icon: "minio",
		Description: "S3-compatible object storage, useful as a backup target for Skifity itself.",
		Website:     "https://min.io",
		Services: []Service{{
			Name: "minio", Image: "quay.io/minio/minio:latest", Port: 9001, Public: true,
			Variables:    map[string]string{"MINIO_BROWSER_REDIRECT_URL": ""},
			Volumes:      []VolumeSpec{{Name: "data", MountPath: "/data", SizeGB: 20}},
			MemRequestMB: 256, MemLimitMB: 1024, CPURequestM: 100, CPULimitM: 1000,
		}},
		Inputs: []Input{
			{Key: "MINIO_ROOT_USER", Label: "Root user", Default: "skifity", Required: true},
			{Key: "MINIO_ROOT_PASSWORD", Label: "Root password", Secret: true, Generate: true},
		},
		Notes: "Create a bucket, then point Settings, then Storage at this MinIO to back up your databases.",
	},
}

// All returns the catalogue.
func All() []Template { return catalogue }

// Lookup finds a template by id.
func Lookup(id string) (Template, bool) {
	for _, t := range catalogue {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// Categories lists the distinct categories, for the filter in the UI.
func Categories() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range catalogue {
		if !seen[t.Category] {
			seen[t.Category] = true
			out = append(out, t.Category)
		}
	}
	return out
}

// Search filters the catalogue by a free-text query.
func Search(query string) []Template {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return catalogue
	}
	var out []Template
	for _, t := range catalogue {
		haystack := strings.ToLower(t.Name + " " + t.Description + " " + t.Category)
		if strings.Contains(haystack, query) {
			out = append(out, t)
		}
	}
	return out
}
