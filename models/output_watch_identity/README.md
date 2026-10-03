# Output watch identity

[`watchDirectory`](../../tools/tsserver-hook-worker.js) registers direct directory watches during `buildResolutionMap` discovery of fragments and generated entrypoints. Atomic file publication changes the file inode; replacing a directory also changes the identity its watch must follow. Reconciliation replaces obsolete handles, and a delayed error from an old handle cannot remove its replacement.

The model has a stable anchor, a replaceable parent and a nested leaf containing one generated file. Three incarnation values distinguish the initial identity and two replacements, which expose a watcher that notices the first atomic publication and loses the next. File publication, parent replacement, leaf deletion/recreation, reconciliation and delayed errors can interleave. `Desired` derives from filesystem facts; only one reconciliation action writes the observed map and reconciles handles. A handle is identified by its subject and directory incarnation. `retired` represents delayed callbacks from closed handles, not production inventory.

| Invariant              | Failure prevented                                                                                                   | Existing regression                                                                                                                                                                                                      |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `FreshWhenSettled`     | A publication changes durable facts without scheduling reconciliation                                               | The plugin journey's first-only → second-only fragment publication retains exact completion membership, definition identity, hover type and diagnostics                                                                  |
| `ReconciledIdentity`   | Settled watches retain replaced directory identities, omit required ancestors, or lose a new handle to an old error | The plugin journey checks rearming after replacement; [`test_worker_map`](../../tests/lsp/worker_map_test.mjs) checks active-error recovery, retired-error fencing and later publication through the replacement handle. |
| `QuiescentConvergence` | A scheduled rebuild never observes a filesystem that has stopped changing                                           | Conditional model property under the assumptions below; the editor journey remains the implementation check                                                                                                              |

Notifications are assumed reliable when the relevant directory handle is attached, and reconciliation is weakly fair. `FinishPublishing` represents the environment stopping filesystem changes; convergence is required after that transition. The parent-replacement action represents moving an ancestor while its old subtree remains intact, so a leaf watch alone receives no notification. `Reconcile` abstracts one synchronous discovery/read/register pass. Only obsolete-handle errors are modeled; registration failures, current-handle failures, inode-number reuse, concurrent filesystem mutation within a pass, platform delivery guarantees and debounce starvation remain outside the model. There is no unconditional convergence claim.

The current-handle error handler calls `scheduleRebuild`; the model's convergence property excludes those active-error traces. The file-inode control retains directory discovery but delivers existing-file publications through the retained file handle, isolating the repeated atomic-publication defect.

Run from this directory using the existing TLC installation:

```sh
java -cp "$TLA2TOOLS_JAR" tlc2.TLC -workers 1 -config Directory.cfg OutputWatchIdentity.tla
```

| Configuration   | Expected result                                                                                                                  |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `Directory.cfg` | All invariants and conditional convergence hold                                                                                  |
| `FileInode.cfg` | `FreshWhenSettled` fails: an existing file watch retains its original inode; two atomic publications can leave the second unseen |
| `LeafOnly.cfg`  | `FreshWhenSettled` fails after replacing an ancestor without an ancestor watch                                                   |
| `Unfenced.cfg`  | `ReconciledIdentity` fails when a delayed old-handle error removes a replacement                                                 |

Use the same command with each control configuration. These are expected outcomes, not execution results. No TLC run, state count, runtime or memory measurement is claimed. The earlier compiler-root, workspace-path and resolver-context models do not establish these watcher properties.
