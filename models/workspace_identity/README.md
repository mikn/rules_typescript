# Workspace identity

The installer compares a workspace and generated artifacts in the same filesystem identity before checking output-directory constraints. An aliased workspace path can otherwise reject a compatible configuration. [`run`](../../tools/copy_to_workspace/main.go) resolves the workspace once before deriving the destination and output-root paths.

The model gives one workspace two filesystem spellings on one volume. Immutable inputs choose the configured root's parent depth, one canonical generated source requiring containment, and whether output-directory options constrain widening. Both spellings use the same common-ancestor preflight and publication guard; only the boundary's workspace identity differs between configurations. Coupled projects retain the physical root when compatible. Uncoupled projects, whose output directories are explicitly disabled, publish the volume root `<<>>`, which contains both workspace spellings and the canonical generated source.

| Invariant | Failure prevented | Installer regression |
| --- | --- | --- |
| `AliasesHaveOneResult` | An alias changes the root or guard decision | `TestRunWorkspaceAliasesCannotWidenCompilerRootOrChangeRelativeOptions` |
| `SupportedContextPublishes` | An uncoupled project retains a narrower root, or a compatible coupled project is rejected | `TestEditorInstalledWorkspaceAliasCannotExcludeAuthoredOrGeneratedSources` and the installer regression's equal-root and ancestor-root cases |
| `ConflictRetainsPrevious` | Actual incompatible widening replaces the previous project | `TestRunPublishesWithoutTruncatingOpenProjects` |

Run `WorkspaceIdentity.tla` with TLC using `Canonical.cfg`; all three invariants
above and the compiler-path invariants below must hold. `Lexical.cfg` must
produce an alias-identity counterexample.

[`throughRoot`](../../ts/tools/tsaction/tsgo.go) maps a compiler-selected path
back to the program layout's source identity. The compiler can report a
checkout's physical path while the source link retains its alias. The model's
`CompilerProjection` accepts both root spellings and leaves paths outside the
workspace absolute.

| Invariant | Failure prevented | Editor regression |
| --- | --- | --- |
| `CompilerSourcesRetainProvenance` | A physical compiler path loses generated or authored source provenance through a checkout alias | `TestEditorGeneratedTypeRootsCannotReadCheckoutTwins`, `TestEditorMixedTypeRootRejectsBeforeInstallation` |
| `ExternalCompilerPathsStayExternal` | Workspace projection redirects an external authored input | `TestEditorPlacementPreservesAbsoluteAuthoredPaths` |

`CompilerLexical.cfg` isolates the compiler-path counterexample: an aliased
workspace fails to recognize a compiler path under the physical workspace.
The finite projection uses two workspace spellings, two relative source paths,
and two external paths, including a sibling with the same workspace-name prefix.

The model covers common-ancestor preflight and publication decisions for nonempty generated containment. The [installer tests](../../tools/copy_to_workspace/main_test.go) separately check real symlinks, byte-identical installation, retained relative options, and atomic publication. The [native editor regression](../../ts/tools/tsaction/editor_test.go) loads the installed project through physical and alias paths, checking source identity, diagnostics and retained `composite` semantics. Alias containment applies to uncoupled projects; coupled projects require the physical workspace path. The compiler projection covers path identity, with real compiler selection and generated-file rejection checked by the editor regressions. Empty containment, cross-volume aliases, filesystem races and full compiler resolution remain outside this model.

## Alias order

`PathsOrder.tla` models the immutable ordered entries passed through build,
editor, and installer projections. `PathsOrdered.cfg` must satisfy:

| Invariant | Failure prevented | Regression |
| --- | --- | --- |
| `OrderedEntriesSurvive` | Projection sorts aliases or candidate lists, or merges a whole-option override | `TestResolve_AliasOrderSurvivesWholeOptionInheritance`, `TestWritePaths_TheChainsMapFromItsWritersDirectory` |
| `NativeWinnerSurvives` | Exact-key, longest-prefix, or first equal-prefix selection changes | `TestEditorEqualPrefixAliasOrderCannotReplaceNativeSelection`, `TestEditorWildcardSuffixKeepsGeneratedTreeResolutionAndAliasPrecedence` |
| `AppendedTiesCannotSteal` | A synthetic wildcard outranks an authored pattern with the same prefix | `TestEditorRefreshedAliasesPreserveAuthoredSiblingsAndRejectDeletedTwins/equal_prefix_suffix_alias` |

The finite inputs include both orders of two overlapping patterns, an exact
key, a longer-prefix pattern, inherited/replaced/empty options, both orders of
two candidates, and each candidate's presence or absence. Locations are tags;
selection compares source identities after relocation. It abstracts compiler
probing and build candidate twins, which the real compiler regressions cover.
It does not model TypeScript resolution generally or generated-subtree overlap
rejection.

`PathsLexical.cfg` checks only `NativeWinnerSurvives`. Sorting at any one of the
three boundaries must expose a different tie winner when key 2 precedes key 1
and a candidate exists. This isolates the old unordered representation without
changing the workspace-identity model or its controls.
