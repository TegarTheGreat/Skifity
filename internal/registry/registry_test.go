package registry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These run against a real HTTP server speaking the registry's v2 API, so the
// requests are built, sent and parsed for real. What is faked is the registry,
// not the client.

func TestRepoAndTag(t *testing.T) {
	cases := []struct {
		image, repo, tag string
		ok               bool
	}{
		// What the panel actually builds: the host has a port, so a naive
		// split on the last colon would take 5000/acme-prod/web as a tag.
		{"skifity-registry.skifity-builds.svc.cluster.local:5000/acme-prod/web:a1b2c3d4e5f6",
			"acme-prod/web", "a1b2c3d4e5f6", true},
		{"registry:5000/acme-prod/web:buildcache", "acme-prod/web", "buildcache", true},
		{"ghcr.io/example/skifity:v1.2.3", "example/skifity", "v1.2.3", true},
		// A reference with no tag is not something a sweep should guess about.
		{"registry:5000/acme-prod/web", "", "", false},
		{"nginx", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		repo, tag, ok := RepoAndTag(c.image)
		if ok != c.ok || repo != c.repo || tag != c.tag {
			t.Errorf("RepoAndTag(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.image, repo, tag, ok, c.repo, c.tag, c.ok)
		}
	}
}

func TestPrunableKeepsWhatCanStillBeReached(t *testing.T) {
	tags := []string{"aaa", "bbb", "ccc", CacheTag}
	keep := map[string]bool{"ccc": true}

	got := Prunable(tags, keep)
	if len(got) != 2 || got[0] != "aaa" || got[1] != "bbb" {
		t.Fatalf("prunable tags are %v, want aaa and bbb", got)
	}

	// The cache tag is never prunable. Removing it does not break anything
	// visible, which is why it would go unnoticed: every app's next build would
	// simply be a cold build.
	for _, tag := range got {
		if tag == CacheTag {
			t.Fatal("the build cache would be deleted, making every next build a cold one")
		}
	}

	// A repository nothing wants is an app that was deleted, and all of it goes
	// except the cache.
	all := Prunable(tags, nil)
	if len(all) != 3 {
		t.Fatalf("a deleted app left %d tags, want all three removed", len(all))
	}
}

func TestSweepingARepository(t *testing.T) {
	deleted := []string{}
	accepts := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/_catalog":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"repositories":["acme-prod/web","gone/app"]}`))

		case strings.HasSuffix(r.URL.Path, "/tags/list"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tags":["old1","old2","current","buildcache"]}`))

		case r.Method == http.MethodHead:
			accepts = r.Header.Get("Accept")
			// The digest is what a delete takes, and it only comes back when
			// the request said which manifest types it understands.
			w.Header().Set("Docker-Content-Digest", "sha256:"+strings.TrimPrefix(
				r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "sha256:"))
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusAccepted)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := New(strings.TrimPrefix(server.URL, "http://"))
	ctx := t.Context()

	repos, err := client.Repositories(ctx)
	if err != nil {
		t.Fatalf("Repositories: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("the catalogue has %d repositories, want 2", len(repos))
	}

	tags, err := client.Tags(ctx, "acme-prod/web")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	prunable := Prunable(tags, map[string]bool{"current": true})
	if len(prunable) != 2 {
		t.Fatalf("prunable is %v, want old1 and old2", prunable)
	}

	for _, tag := range prunable {
		digest, err := client.Digest(ctx, "acme-prod/web", tag)
		if err != nil {
			t.Fatalf("Digest(%s): %v", tag, err)
		}
		if err := client.DeleteManifest(ctx, "acme-prod/web", digest); err != nil {
			t.Fatalf("DeleteManifest(%s): %v", tag, err)
		}
	}

	if len(deleted) != 2 {
		t.Fatalf("%d manifests were deleted, want 2", len(deleted))
	}
	// Without this header the registry answers with the digest of a converted
	// schema-1 manifest, and deleting that either fails or removes the wrong
	// thing.
	if !strings.Contains(accepts, "application/vnd.oci.image.manifest.v1+json") {
		t.Errorf("the digest was asked for without naming the manifest types: %q", accepts)
	}
}

func TestARegistryThatRefusesDeletesSaysSo(t *testing.T) {
	// The registry is started with deletion enabled; if somebody changes that,
	// the sweep must not report success while the disk stays exactly as full.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	client := New(strings.TrimPrefix(server.URL, "http://"))
	err := client.DeleteManifest(t.Context(), "acme-prod/web", "sha256:abc")
	if err == nil {
		t.Fatal("a registry that refuses deletes reported success")
	}
	if !strings.Contains(err.Error(), "REGISTRY_STORAGE_DELETE_ENABLED") {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
}

func TestAnAppThatWasNeverBuiltIsNotAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tags, err := New(strings.TrimPrefix(server.URL, "http://")).Tags(t.Context(), "acme-prod/web")
	if err != nil {
		t.Fatalf("a repository that does not exist gave %v, want no error", err)
	}
	if len(tags) != 0 {
		t.Fatalf("got %v tags for a repository that does not exist", tags)
	}
}
