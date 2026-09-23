### Fixed

Workspace hoist settings now read `pnpm-workspace.yaml`, overriding corresponding `.npmrc` values as in pnpm 11. Public patterns produce root links for first-party runtime imports; private hoisting retains its existing defaults.
