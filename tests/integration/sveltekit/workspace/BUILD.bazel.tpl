"""Root BUILD file for the sveltekit_build integration test workspace.

Everything else at this level -- node_modules and sveltekit_build -- is what
Gazelle generates from the @sveltejs/kit dep in package.json. That is the point
of the test: SvelteKit used to be refused outright, so the generated wiring is
half of what "supported" has to mean.
"""

load("@gazelle//:def.bzl", "gazelle")

gazelle(
    name = "gazelle",
    gazelle = "@rules_typescript//gazelle:gazelle_typescript",
)
