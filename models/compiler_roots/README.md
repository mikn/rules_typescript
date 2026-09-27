# Compiler-owned editor roots

[`editorRun`](../../ts/tools/tsaction/editor.go) writes compiler-observed roots as
literal `files` entries, with empty `include` and `exclude`. The compiler has
already applied exclusion and extension-priority rules. Reinterpreting an
observed filename as an include pattern can admit an unobserved sibling:
`a?.ts` matches both the literal filename and `ab.ts`.
Refresh refuses an explicit generated root without an exact loaded identity,
preserving the previous project. This includes missing children of declared
generated trees and extensionless requests whose loaded filename differs.

The immutable inputs range over two identities and distinguish loaded roots,
explicit requests, and missing generated requests. A missing request belongs to
the explicit set and cannot be loaded: each identity has five possible states,
giving 25 valuations. Serialization assigns each identity its filename;
interpretation keeps `files` literal and expands `include` patterns. Missing
identities can retain diagnostics but cannot contribute loaded sources. `Next`
stutters; `Publish` models the refusal before replacing a project.

| Invariant | Failure prevented | Regression |
| --- | --- | --- |
| `CompilerOwnsMembership` | An unobserved root enters the editor, or an observed root disappears | `TestEditorLiteralRootsCannotAdmitGlobMatchingSiblings` checks authored `?` and generated `*` filenames against undeclared matching siblings; `TestEditorBroadIncludeRetainsOnlyCompilerGeneratedRoots` checks stale explicit roots and generated removal/recreation |
| `ObservedExplicitRootsStayExplicit` | Exclusions hide an observed explicit root | Both membership regressions retain excluded `pinned.ts`; `TestEditorPlacementPreservesAbsoluteAuthoredPaths` checks physical identity inside and outside the workspace |
| `MissingGeneratedRootsStayExplicit` | Publishing drops a missing generated root's diagnostic identity | `TestEditorBroadIncludeRetainsOnlyCompilerGeneratedRoots` deletes and recreates a child of a still-declared tree, checks refusal leaves the prior project byte-identical with canonical TS6053, and excludes its stale checkout twin |

Run from this directory with `TLA2TOOLS_JAR` pointing to TLC:

```sh
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config Observed.cfg CompilerRoots.tla
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config LegacyPatterns.cfg CompilerRoots.tla
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config LegacyCopy.cfg CompilerRoots.tla
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -config LegacyMissing.cfg CompilerRoots.tla
```

`Observed.cfg` must complete with all invariants satisfied. `LegacyPatterns.cfg`
must fail `CompilerOwnsMembership`: compiler roots `{literal}` and no explicit
roots serialize `a?.ts` into `include`, which also selects `sibling`.
`LegacyCopy.cfg` isolates unconditional copying into the literal root list and
must fail the same invariant, such as empty compiler roots and explicit roots
`{sibling}`. Its failure cannot come from wildcard expansion.
`LegacyMissing.cfg` must fail `MissingGeneratedRootsStayExplicit`: empty loaded
roots and an explicit missing generated root publish a project without that
root's diagnostic identity.

These are expected outcomes, not execution results. The model assumes the
compiler supplies observed identities and models missing generated roots rather
than extension substitution. Compiler parsing, path relocation, atomic filesystem
publication, and editor invalidation remain outside this model.
