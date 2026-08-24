package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The alias comes from an importer-relative `link:` entry in pnpm-lock.yaml
// (importer tests/npm/nested → link:../../../packages/nested-shared), so its
// files can only land in runfiles when the link path is resolved against the
// importer rather than treated as root-relative.
func TestWorkspaceRelativeLink(t *testing.T) {
	verify.New(t).FoundFile("*/packages/nested-shared/index.d.ts").Exists()
}
