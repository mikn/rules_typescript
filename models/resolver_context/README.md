# Resolver context

This model checks one editor-project publication. It distinguishes an artifact
outside the checkout from a compiler source that requires `rootDir` containment.
Declaration/config artifacts do not widen the root. Widening can change package
imports and self-references when output-directory options are active. Relocating a
package scope can independently change module format or package-relative queries.
Moving the effective config inside a package can also enable output-to-source
remapping while `rootDir` stays unchanged.

`containment` represents excluded files, proven program roots, unknown non-root
eligibility, or absent producer facts. `needsContainment` is the consumer truth;
only the first two classes constrain it. `known` and `coupled` describe effective
output options. `scope` distinguishes module-format-only authored manifests,
authored path-bearing manifests, and generated package scopes. `members` records
authored and generated members inside the scope, plus unrelated outside members;
`manifestPathPreserved` records whether the layout retains the producer manifest
at its canonical package path. `configBefore` and `configAfter` record whether
the package scope contains the effective project file before and after projection;
`packageMapping` distinguishes absent maps, first-party maps and npm scopes.
Compatibility derives from those facts: no retained authored member inside a
generated scope and no manifest relocation. Scalar and tree artifacts share this
rule. `materialized` means the required scope retains its producer-owned bytes.
Two queries distinguish observed and unobserved resolution; every identity
invariant quantifies over both.

The `project` and `expanded` root tags abstract required containment and
package-mapping context. [`WorkspaceIdentity`](../workspace_identity/README.md)
and `TestEditorInstalledWorkspaceAliasCannotExcludeAuthoredOrGeneratedSources`
cover the installed volume root and lexical workspace aliases.

The corrected transition rejects unknown eligibility, missing facts needed for
outside-root binding, incompatible root changes, and changed package context.
Supported contexts publish, including complete generated scalar/tree namespaces,
unrelated authored siblings outside their scope, and declaration-only artifacts
with active output options when config/package containment is unchanged. Conflicts
retain the previous project bytes.

[`editor.go`](../../ts/tools/tsaction/editor.go) derives the containment fact from
the compiler listing and checks declared package scopes. Existing staging in
[`ts_compile.bzl`](../../ts/private/rules/ts_compile.bzl) materializes module-format
facts. [`canonicalEditorPaths`](../../tools/copy_to_workspace/main.go) binds these
facts to canonical paths, removes them from the installed config, and either
returns complete bytes or fails before publication. The existing overlay writer
checks namespace compatibility before installing a generated scope in the
compiler program; authored members also need their module context preserved.
The same layout owner checks declared authored, scalar and tree package scopes
against effective config placement before producing an editor artifact.
Immutable declared inputs, a nonempty authored `rootDir`, and atomic publication
of one project are assumptions.

Run TLC with an existing Java runtime and `TLA2TOOLS_JAR` pointing to TLC 1.8.0:

```sh
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config Old.cfg ResolverContext.tla
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config Observed.cfg ResolverContext.tla
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config Retain.cfg ResolverContext.tla
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config AssumedNamespace.cfg ResolverContext.tla
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config RetainPlacement.cfg ResolverContext.tla
```

Check the corrected transition:

```sh
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config ContextFence.cfg ResolverContext.tla
```

| Configuration | Required outcome |
| --- | --- |
| `Old.cfg` | `PreservesSourceIdentity` counterexample |
| `Observed.cfg` | `PreservesSourceIdentity` counterexample despite checking observed queries |
| `Retain.cfg` | `ContainsGeneratedSources` counterexample |
| `AssumedNamespace.cfg` | `PreservesPackageContext` counterexample from mixed members or a relocated manifest |
| `RetainPlacement.cfg` | `PreservesPackageContext` counterexample without root widening, including an unobserved query |
| `ContextFence.cfg` | Exhaustive completion of all ten invariants |

The three root-context modes restrict inputs to known root containment and preserved
module scope, isolating their original root-context defect. The assumed-namespace
mode isolates generated scopes with compatible root options while allowing mixed
members and relocated manifests. The placement mode isolates declaration-only
inputs with active output options and a changed config/package ancestor predicate.
The corrected mode explores every declared fact class. Publication and unknown-context invariants
prevent a reject-all implementation or false containment facts from passing.

The compiler excludes targets under `node_modules` from local output-to-source
remapping. The model represents npm scopes supplied by the declared importer chain,
which contain neither project file; it does not model arbitrary project files
inside an npm installation. `TestEditorNpmFallbackPreservesImporterResolutionModes`
pins the existing conditional-npm positive alongside generated declarations.

Expected outcomes are not run results. This finite model does not establish
compiler conformance, other projections, filesystem races, multi-project
atomicity, watching, or same-session editor refresh. Existing compiler and
installer regressions qualify the implementation separately.
