package settings

import "testing"

// A setting whose kind does not match its validator is a form that accepts what
// the server will reject: a text box for a yes/no value, or a free-text field
// for something that must be one of four words.
func TestEveryDefinitionHasAKindItsValidatorAgrees(t *testing.T) {
	for _, def := range Definitions {
		t.Run(def.Key, func(t *testing.T) {
			kind := def.ResolvedKind()

			switch kind {
			case KindBool:
				// The switch writes exactly these two, so the validator has to
				// take exactly these two.
				for _, value := range []string{"true", "false"} {
					if def.Validate != nil {
						if err := def.Validate(value); err != nil {
							t.Errorf("a switch writes %q, which the validator rejects: %v", value, err)
						}
					}
				}
				if def.Validate != nil {
					if err := def.Validate("yes"); err == nil {
						t.Error("a bool setting should not accept free text such as \"yes\"")
					}
				}
			case KindChoice:
				if len(def.Options) == 0 {
					t.Fatal("a choice setting with no options is an empty dropdown")
				}
				for _, option := range def.Options {
					if def.Validate != nil {
						if err := def.Validate(option); err != nil {
							t.Errorf("option %q is offered but rejected: %v", option, err)
						}
					}
				}
				if def.Validate != nil {
					if err := def.Validate("definitely-not-an-option"); err == nil {
						t.Error("a choice setting accepted a value that is not one of its options")
					}
				}
			case KindNumber:
				if def.Validate != nil {
					if err := def.Validate("12"); err != nil {
						t.Errorf("a number field rejects a number: %v", err)
					}
					if err := def.Validate("not a number"); err == nil {
						t.Error("a number setting accepted text")
					}
				}
			}

			// Clearing a setting is how an integration is disconnected, so an
			// empty value must always be allowed whatever the kind.
			if def.Validate != nil {
				if err := def.Validate(""); err != nil {
					t.Errorf("an empty value must be allowed, so a setting can be cleared: %v", err)
				}
			}

			if def.Label == "" || def.Help == "" || def.Group == "" {
				t.Error("every setting needs a label, a group and a line of help")
			}
		})
	}
}

// A secret that is also a switch, or a choice with no options, is a control the
// panel cannot draw.
func TestKindsAreCoherent(t *testing.T) {
	for _, def := range Definitions {
		kind := def.ResolvedKind()
		if def.Secret && kind == KindBool {
			t.Errorf("%s: a secret cannot be a switch", def.Key)
		}
		if len(def.Options) > 0 && kind != KindChoice {
			t.Errorf("%s: has options but is drawn as %s", def.Key, kind)
		}
		if def.Multiline && kind != KindText {
			t.Errorf("%s: only a text setting can be multiline, got %s", def.Key, kind)
		}
	}
}
