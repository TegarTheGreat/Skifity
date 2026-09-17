package templates

import (
	"regexp"
	"strings"
	"testing"

	"skifity/internal/kube"
)

// The engines Skifity provisions, spelled out here rather than imported.
//
// internal/dbsvc reaches internal/api, which reaches this package, so
// importing it from here would be a cycle. That the two lists agree is checked
// where both are already in scope, in internal/api.
var engines = map[string]bool{"postgres": true, "mysql": true, "redis": true}

// The catalogue is data, and data goes wrong quietly.
//
// A template is installed with one click by somebody who has not read it, so a
// mistake in it is not a compile error or a failing request: it is a database
// that is created and never linked, or an app that comes up as software nobody
// chose. These check the shape of every entry, because the catalogue is the one
// part of this product a user runs without looking at first.

func TestEveryTemplateIsComplete(t *testing.T) {
	seen := map[string]string{}
	for _, tpl := range All() {
		if tpl.ID == "" {
			t.Fatalf("a template has no id: %+v", tpl)
		}
		if other, clash := seen[tpl.ID]; clash {
			t.Errorf("%s and %s share the id %q, so Lookup returns whichever is first", other, tpl.Name, tpl.ID)
		}
		seen[tpl.ID] = tpl.Name

		if kube.Slugify(tpl.ID) != tpl.ID {
			t.Errorf("%s: the id %q is not already a slug, and it ends up in a URL", tpl.Name, tpl.ID)
		}
		for field, value := range map[string]string{
			"name": tpl.Name, "description": tpl.Description,
			"category": tpl.Category, "website": tpl.Website,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s has no %s, and the card shows it", tpl.ID, field)
			}
		}
		if !strings.HasPrefix(tpl.Website, "https://") {
			t.Errorf("%s links to %q; a link out of the panel must be https", tpl.ID, tpl.Website)
		}
		if len(tpl.Services) == 0 {
			t.Errorf("%s installs nothing", tpl.ID)
		}
	}
}

func TestEveryServiceCouldRun(t *testing.T) {
	for _, tpl := range All() {
		public := 0
		names := map[string]bool{}
		for _, svc := range tpl.Services {
			if svc.Name == "" || kube.Slugify(svc.Name) != svc.Name {
				t.Errorf("%s: the service name %q is not a slug, and it becomes a Kubernetes object", tpl.ID, svc.Name)
			}
			if names[svc.Name] {
				t.Errorf("%s: two services are called %q, and the second takes the first's Service over", tpl.ID, svc.Name)
			}
			names[svc.Name] = true

			// A worker does not listen, and Skifity runs one as an app with
			// no port: no Service, no probes, no ingress. Inventing a port for
			// a Sidekiq gives it a readiness check against something that
			// never answers, and an app that is "starting" forever.
			if svc.Port == 0 {
				if svc.Public {
					t.Errorf("%s/%s is public and listens on nothing", tpl.ID, svc.Name)
				}
				if svc.HealthPath != "" {
					t.Errorf("%s/%s has a health path and no port to check it on", tpl.ID, svc.Name)
				}
			} else if svc.Port < 1 || svc.Port > 65535 {
				t.Errorf("%s/%s listens on %d", tpl.ID, svc.Name, svc.Port)
			}
			if svc.Public {
				public++
			}
			if svc.HealthPath != "" && !strings.HasPrefix(svc.HealthPath, "/") {
				t.Errorf("%s/%s has the health path %q, which is not a path", tpl.ID, svc.Name, svc.HealthPath)
			}
			if svc.MemLimitMB > 0 && svc.MemRequestMB > svc.MemLimitMB {
				t.Errorf("%s/%s asks for more memory than it is allowed, so it can never be scheduled", tpl.ID, svc.Name)
			}
			if svc.CPULimitM > 0 && svc.CPURequestM > svc.CPULimitM {
				t.Errorf("%s/%s asks for more CPU than it is allowed", tpl.ID, svc.Name)
			}
			for key := range svc.Variables {
				if _, err := kube.SanitiseEnvKey(key); err != nil {
					t.Errorf("%s/%s sets %q, which a container cannot carry: %v", tpl.ID, svc.Name, key, err)
				}
			}
			for _, vol := range svc.Volumes {
				if !strings.HasPrefix(vol.MountPath, "/") {
					t.Errorf("%s/%s mounts %q, which is not an absolute path", tpl.ID, svc.Name, vol.MountPath)
				}
				if vol.SizeGB < 0 {
					t.Errorf("%s/%s asks for %d GB", tpl.ID, svc.Name, vol.SizeGB)
				}
			}
		}
		if public == 0 {
			t.Errorf("%s has no public service, so nothing it installs can be opened", tpl.ID)
		}
	}
}

// TestNoWorkerIsTreatedAsAWebApp: a queue consumer, a Sidekiq, a scheduler —
// nothing about it answers HTTP. Giving one a domain produces a certificate, an
// ingress rule and a readiness probe pointed at a port that will never open,
// and the app stays "starting" until somebody reads the events. The converter
// that built this catalogue marked one worker public twice before this existed.
func TestNoWorkerIsTreatedAsAWebApp(t *testing.T) {
	worker := regexp.MustCompile(
		`(^|[-_])(workers?|sidekiq|celery|beat|scheduler|cron|queue|consumer|` +
			`runners?|supervisor|jobs?)([-_]|$)`)
	for _, tpl := range All() {
		for _, svc := range tpl.Services {
			if worker.MatchString(svc.Name) && svc.Public {
				t.Errorf("%s/%s is named for a worker and is public; a queue consumer has no page to open",
					tpl.ID, svc.Name)
			}
		}
	}
}

// TestEveryDatabaseReachesTheServiceItIsFor: a LinkTo that names no service is
// the worst kind of mistake here, because everything appears to work. The
// database is created, the link is skipped, and the app starts without the one
// variable it cannot run without — and crash-loops with nothing on screen
// saying why.
func TestEveryDatabaseReachesTheServiceItIsFor(t *testing.T) {
	for _, tpl := range All() {
		services := map[string]bool{}
		for _, svc := range tpl.Services {
			services[svc.Name] = true
		}
		for _, db := range tpl.Databases {
			if db.Name == "" || kube.Slugify(db.Name) != db.Name {
				t.Errorf("%s: the database name %q is not a slug", tpl.ID, db.Name)
			}
			if !engines[db.Engine] {
				t.Errorf("%s: %q is not an engine Skifity runs", tpl.ID, db.Engine)
			}
			if db.StorageGB <= 0 {
				t.Errorf("%s/%s asks for %d GB of storage", tpl.ID, db.Name, db.StorageGB)
			}
			if len(db.LinkTo) == 0 {
				t.Errorf("%s: the database %s is created and linked to nothing", tpl.ID, db.Name)
			}
			for _, target := range db.LinkTo {
				if !services[target] {
					t.Errorf("%s: the database %s links to %q, which is not a service in this template",
						tpl.ID, db.Name, target)
				}
			}
			if db.VarName != "" {
				if _, err := kube.SanitiseEnvKey(db.VarName); err != nil {
					t.Errorf("%s/%s arrives as %q, which a container cannot carry: %v",
						tpl.ID, db.Name, db.VarName, err)
				}
			}
		}
	}
}

// floatingTag matches a tag that means "whatever is newest".
//
// `latest` is only the most honest spelling of it. `main`, `main-stable`,
// `16-master`, `release` and `postgresql-edge` all move under the app, and the
// first version of this caught none of them: litellm reached the catalogue on
// `main-stable`, which is a branch with a nicer name.
var floatingTag = regexp.MustCompile(`(^|[-_.])(latest|main|master|stable|edge|nightly|release|dev)$`)

// TestNoTemplateRunsWhateverIsNewest: an image on a floating tag is not a
// version. Two deploys of the same app run different software, a rollback
// restores a tag rather than the thing that worked, and an upstream release
// arrives on a restart nobody asked for. The product promises rollback, so the
// catalogue has to name what it runs.
func TestNoTemplateRunsWhateverIsNewest(t *testing.T) {
	for _, tpl := range All() {
		for _, svc := range tpl.Services {
			// A registry host may carry a port, so the tag is after the last
			// colon and only if there is no slash after it.
			colon := strings.LastIndex(svc.Image, ":")
			if colon < 0 || strings.Contains(svc.Image[colon:], "/") {
				t.Errorf("%s/%s runs %q with no tag, which means latest", tpl.ID, svc.Name, svc.Image)
				continue
			}
			if tag := svc.Image[colon+1:]; floatingTag.MatchString(tag) {
				t.Errorf("%s/%s runs %q: %q is whatever is newest, so this app cannot be rolled back",
					tpl.ID, svc.Name, svc.Image, tag)
			}
		}
	}
}

// An input that is neither required, nor generated, nor defaulted is a field
// the installer asks for and then does nothing about when it is empty.
func TestEveryInputIsAskedForOrFilledIn(t *testing.T) {
	for _, tpl := range All() {
		for _, input := range tpl.Inputs {
			if input.Key == "" || input.Label == "" {
				t.Errorf("%s has an input with no key or no label: %+v", tpl.ID, input)
			}
			if _, err := kube.SanitiseEnvKey(input.Key); err != nil {
				t.Errorf("%s asks for %q, which a container cannot carry: %v", tpl.ID, input.Key, err)
			}
			if !input.Required && !input.Generate && input.Default == "" {
				t.Errorf("%s asks for %s and does nothing when it is left empty", tpl.ID, input.Key)
			}
			if input.Generate && input.Default != "" {
				t.Errorf("%s/%s is both generated and defaulted; the default wins and the generator never runs",
					tpl.ID, input.Key)
			}
		}
	}
}

func TestSearchAndLookupFindWhatIsThere(t *testing.T) {
	for _, tpl := range All() {
		found, ok := Lookup(tpl.ID)
		if !ok || found.ID != tpl.ID {
			t.Errorf("Lookup(%q) did not find it", tpl.ID)
		}
		if len(Search(tpl.Name)) == 0 {
			t.Errorf("searching for %q finds nothing, and it is the name on the card", tpl.Name)
		}
	}
	if len(Search("")) != len(All()) {
		t.Error("an empty search hides templates")
	}
	if len(Search("nothing-is-called-this")) != 0 {
		t.Error("a search that matches nothing returned something")
	}
	if len(Categories()) == 0 {
		t.Error("there are no categories, and the page groups by them")
	}
}

// TestNoTemplatePointsAtAContainerThatIsNotThere: a Compose file wires services
// together by service name — DB_HOST=mariadb, REDIS_HOST=redis — and a template
// converted from one carries those over unless something stops it. In Skifity
// there is no sibling container to point at: the database is a managed one and
// arrives as a URL through the link. An app given the old variables starts,
// fails to resolve a hostname nobody recognises, and crash-loops.
//
// This caught bookstack, glpi, metabase, redmine and keycloak on the first
// import, which is a fifth of the templates that bring a database.
func TestNoTemplatePointsAtAContainerThatIsNotThere(t *testing.T) {
	datastore := regexp.MustCompile(
		`(^|_)(DB|DATABASE|POSTGRES|POSTGRESQL|PG|MYSQL|MARIADB|REDIS|VALKEY|KEYDB|MONGO|MONGODB|` +
			`CACHE|QUEUE|BROKER|AMQP|RABBITMQ|ELASTIC|ELASTICSEARCH|MEILI|CLICKHOUSE)($|_)`)
	address := regexp.MustCompile(`(^|_)(HOST|HOSTNAME|PORT|SERVER|ADDR|ADDRESS|URL|URI|DSN|CONNECTION|CONNECTIONSTRING)$`)

	for _, tpl := range All() {
		for _, svc := range tpl.Services {
			for key, value := range svc.Variables {
				upper := strings.ToUpper(key)
				if datastore.MatchString(upper) && address.MatchString(upper) {
					t.Errorf("%s/%s sets %s=%q; Skifity injects a connection string instead, "+
						"and this points at a container that does not exist",
						tpl.ID, svc.Name, key, value)
				}
				// A value the source file expected a shell to expand is not a
				// value; it reaches the container as the literal text.
				if strings.Contains(value, "$") {
					t.Errorf("%s/%s sets %s=%q, which was never expanded", tpl.ID, svc.Name, key, value)
				}
			}
		}
	}
}

// The catalogue is read from files at startup. A file that does not parse is a
// build-time mistake and has to fail here rather than at run time, where it
// would be an empty Templates page and no reason for it.
func TestTheCatalogueLoads(t *testing.T) {
	if err := Err(); err != nil {
		t.Fatalf("the catalogue could not be read: %v", err)
	}
	if len(All()) < 100 {
		t.Fatalf("the catalogue has %d templates; the files are not being embedded", len(All()))
	}
}
