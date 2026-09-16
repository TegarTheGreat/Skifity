package serverapp

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"skifity/internal/store"
)

// A deployment that was running when the panel stopped must not sit in the
// interface as though it were still going. Nothing will ever finish it: the
// goroutine driving it died with the process.
func TestInterruptedDeploymentsAreMarkedFailed(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	defer db.Close()

	app := seedApp(t, db)

	// One of each state the panel can be interrupted in, plus one that had
	// already finished and must be left alone.
	running := map[store.DeploymentStatus]string{}
	for _, status := range []store.DeploymentStatus{store.DeployQueued, store.DeployBuilding, store.DeployDeploying} {
		deployment := store.Deployment{AppID: app.ID, Status: status, Trigger: "manual"}
		if err := db.CreateDeployment(ctx, &deployment); err != nil {
			t.Fatalf("create a %s deployment: %v", status, err)
		}
		running[status] = deployment.ID
	}
	finished := store.Deployment{AppID: app.ID, Status: store.DeploySucceeded, Trigger: "manual"}
	if err := db.CreateDeployment(ctx, &finished); err != nil {
		t.Fatalf("create a finished deployment: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := markInterruptedDeployments(ctx, db, log); err != nil {
		t.Fatalf("mark interrupted deployments: %v", err)
	}

	for status, id := range running {
		deployment, err := db.GetDeployment(ctx, id)
		if err != nil {
			t.Fatalf("read back the %s deployment: %v", status, err)
		}
		if deployment.Status != store.DeployFailed {
			t.Errorf("a %s deployment should have been failed, got %q", status, deployment.Status)
		}
		// A failure with no explanation is the thing this product exists to
		// avoid: the user has to be told why it stopped and what to do.
		if deployment.ErrorMessage == "" {
			t.Errorf("the %s deployment has no explanation", status)
		}
		if deployment.ErrorHint == "" {
			t.Errorf("the %s deployment has no suggested fix", status)
		}
	}

	unchanged, err := db.GetDeployment(ctx, finished.ID)
	if err != nil {
		t.Fatalf("read back the finished deployment: %v", err)
	}
	if unchanged.Status != store.DeploySucceeded {
		t.Errorf("a deployment that had already finished was changed to %q", unchanged.Status)
	}

	// Running it again must change nothing: the panel restarts, and a restart
	// loop must not rewrite history each time.
	remaining, err := db.ListUnfinishedDeployments(ctx)
	if err != nil {
		t.Fatalf("list unfinished deployments: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected nothing left unfinished, got %d", len(remaining))
	}
}

// seedApp creates the minimum chain a deployment needs to exist: a team, a
// project, an environment and an app.
func seedApp(t *testing.T, db *store.DB) store.App {
	t.Helper()
	ctx := context.Background()

	team := store.Team{Name: "Acme", Slug: "acme"}
	if err := db.CreateTeam(ctx, &team); err != nil {
		t.Fatalf("create a team: %v", err)
	}
	project := store.Project{TeamID: team.ID, Name: "Website", Slug: "website"}
	if err := db.CreateProject(ctx, &project); err != nil {
		t.Fatalf("create a project: %v", err)
	}
	environment := store.Environment{
		ProjectID: project.ID, Name: "Production", Slug: "production",
		Kind: "standard", Namespace: "t-website-production",
	}
	if err := db.CreateEnvironment(ctx, &environment); err != nil {
		t.Fatalf("create an environment: %v", err)
	}
	app := store.App{
		EnvironmentID: environment.ID, Name: "web", Slug: "web",
		SourceType: "git", RepoURL: "https://example.test/web.git", Status: "created",
	}
	if err := db.CreateApp(ctx, &app); err != nil {
		t.Fatalf("create an app: %v", err)
	}
	return app
}
