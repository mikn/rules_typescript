package css_module_composes_test

import (
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// A composed name has to be the name the OTHER target computed, or the class the
// browser gets is not the class that file's stylesheet defines.
func TestComposedNameIsTheDepsOwnName(t *testing.T) {
	tree := verify.New(t)

	theme := map[string]string{}
	tree.File("tests/css_module_composes/theme.module.css.exports.json").JSON(&theme)
	card := map[string]string{}
	tree.File("tests/css_module_composes/card.module.css.exports.json").JSON(&card)

	names := strings.Fields(card["card"])
	if len(names) != 2 {
		t.Fatalf("card composes one class, so its value is two names; got %q", card["card"])
	}
	if names[1] != theme["accent"] {
		t.Errorf("card composes %q, theme:accent is %q", names[1], theme["accent"])
	}
	if names[0] == names[1] {
		t.Errorf("both names are %q; each file's hash is over its own bytes", names[0])
	}

	// The composed-from name is the dep's, so it is not among this file's keys.
	tree.File("tests/css_module_composes/card.module.css.d.ts").
		ExcludesRE(`readonly "?accent"?:`)
}
