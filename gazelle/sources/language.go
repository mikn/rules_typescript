package sources

import (
	"github.com/bazelbuild/bazel-gazelle/language"
	typescript "github.com/mikn/rules_typescript/gazelle"
)

func NewLanguage() language.Language { return typescript.NewSourceInventoryLanguage() }
