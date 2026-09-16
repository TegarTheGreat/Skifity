package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInterspersed(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantApp    string
		wantSecret bool
		wantPos    []string
	}{
		{"flags first", []string{"--app", "web", "KEY=value"}, "web", false, []string{"KEY=value"}},
		{"flags last", []string{"KEY=value", "--app", "web"}, "web", false, []string{"KEY=value"}},
		{"flags between", []string{"A=1", "--app", "web", "B=2"}, "web", false, []string{"A=1", "B=2"}},
		{"boolean flag", []string{"KEY=value", "--secret"}, "", true, []string{"KEY=value"}},
		{"no flags", []string{"A=1", "B=2"}, "", false, []string{"A=1", "B=2"}},
		{"no positionals", []string{"--app", "web"}, "web", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			flags.SetOutput(os.Stderr)
			app := flags.String("app", "", "")
			secret := flags.Bool("secret", false, "")

			positional, err := parseInterspersed(flags, tc.args)
			if err != nil {
				t.Fatalf("parseInterspersed: %v", err)
			}
			if *app != tc.wantApp {
				t.Errorf("app is %q, want %q", *app, tc.wantApp)
			}
			if *secret != tc.wantSecret {
				t.Errorf("secret is %v, want %v", *secret, tc.wantSecret)
			}
			if strings.Join(positional, ",") != strings.Join(tc.wantPos, ",") {
				t.Errorf("positional is %v, want %v", positional, tc.wantPos)
			}
		})
	}
}

func TestParseProjectFile(t *testing.T) {
	file, err := parseProjectFile(`
# a comment
name = "shop"
app = "app_123"
environment = "env_456"
`)
	if err != nil {
		t.Fatalf("parseProjectFile: %v", err)
	}
	if file.App != "app_123" || file.Name != "shop" || file.Environment != "env_456" {
		t.Fatalf("parsed as %+v", file)
	}

	// An unknown key is an error rather than being ignored: a typo in a
	// setting the user believes they changed is worse than a clear failure.
	if _, err := parseProjectFile(`aap = "app_123"`); err == nil {
		t.Fatal("an unknown setting was accepted")
	}
	if _, err := parseProjectFile(`app`); err == nil {
		t.Fatal("a line with no value was accepted")
	}
}

func TestProjectFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	path, err := SaveProjectFile(ProjectFile{App: "app_1", Name: "Shop", Environment: "env_1"})
	if err != nil {
		t.Fatalf("SaveProjectFile: %v", err)
	}
	// It is committed to a repository, so it must contain nothing secret.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(data), "token") {
		t.Fatalf("the project file contains a credential:\n%s", data)
	}

	// It must be found from a subdirectory, the way git finds its root.
	sub := filepath.Join(dir, "packages", "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	found, _, err := LoadProjectFile()
	if err != nil {
		t.Fatalf("LoadProjectFile from a subdirectory: %v", err)
	}
	if found.App != "app_1" {
		t.Fatalf("found %+v", found)
	}
}

func TestNormaliseRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/shop.git":      "https://github.com/acme/shop",
		"https://github.com/acme/shop.git":  "https://github.com/acme/shop",
		"https://github.com/acme/shop":      "https://github.com/acme/shop",
		"git@gitlab.example.com:team/x.git": "https://gitlab.example.com/team/x",
	}
	for input, want := range cases {
		if got := normaliseRemote(input); got != want {
			t.Errorf("normaliseRemote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestConfigIsPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("SKIFITY_CONFIG", path)

	if err := SaveConfig(Config{PanelURL: "https://panel.example.test", Token: "skf_secret"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// A token readable by every user on the machine is not a stored token.
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("the config file is mode %#o", mode)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Token != "skf_secret" {
		t.Fatalf("the token did not survive: %q", loaded.Token)
	}
}

func TestLoadConfigWithoutOneExplainsItself(t *testing.T) {
	t.Setenv("SKIFITY_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("a missing config was accepted")
	}
	if !strings.Contains(err.Error(), "login") {
		t.Fatalf("the error does not tell the user what to do: %v", err)
	}
}

func TestQuery(t *testing.T) {
	if got := Query("tail", "100", "follow", ""); got != "?tail=100" {
		t.Fatalf("Query dropped or kept the wrong values: %q", got)
	}
	if got := Query("a", ""); got != "" {
		t.Fatalf("Query with nothing set = %q, want empty", got)
	}
}

func TestEnvSubcommand(t *testing.T) {
	cases := []struct {
		args     []string
		wantSub  string
		wantVals []string
	}{
		{nil, "list", nil},
		{[]string{"list"}, "list", nil},
		{[]string{"ls"}, "list", nil},
		{[]string{"set", "A=1", "B=2"}, "set", []string{"A=1", "B=2"}},
		{[]string{"unset", "A"}, "unset", []string{"A"}},
		{[]string{"rm", "A"}, "unset", []string{"A"}},
		// A bare KEY=value is what someone typing it means.
		{[]string{"A=1"}, "set", []string{"A=1"}},
		{[]string{"nonsense"}, "nonsense", nil},
	}
	for _, tc := range cases {
		sub, values := envSubcommand(tc.args)
		if sub != tc.wantSub {
			t.Errorf("envSubcommand(%v) sub = %q, want %q", tc.args, sub, tc.wantSub)
		}
		if strings.Join(values, ",") != strings.Join(tc.wantVals, ",") {
			t.Errorf("envSubcommand(%v) values = %v, want %v", tc.args, values, tc.wantVals)
		}
	}
}
