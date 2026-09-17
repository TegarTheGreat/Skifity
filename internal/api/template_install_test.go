package api

import (
	"testing"

	"skifity/internal/templates"
)

// A template that does not name the variable its database arrives as falls back
// to a default per engine. A missing default links the app to its database
// through a variable called "", which the app cannot read and nothing reports.
//
// The engine names are literals because internal/dbsvc, which defines them,
// imports this package. That the two lists agree is checked in
// template_engines_test.go, which is an external test package and can import
// both.
func TestEveryEngineHasADefaultVariableName(t *testing.T) {
	for _, engine := range []string{"postgres", "mysql", "redis"} {
		if defaultVarNameFor(engine) == "" {
			t.Errorf("a %s database with no var_name would arrive as an empty variable name", engine)
		}
	}
}

// Every template's databases must name an engine that has such a default.
func TestEveryTemplateDatabaseArrivesAsAVariable(t *testing.T) {
	for _, tpl := range templates.All() {
		for _, db := range tpl.Databases {
			name := db.VarName
			if name == "" {
				name = defaultVarNameFor(db.Engine)
			}
			if name == "" {
				t.Errorf("the %s template's %s database would arrive as an empty variable name",
					tpl.ID, db.Name)
			}
		}
	}
}
