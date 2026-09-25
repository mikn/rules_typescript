### Fixed

- Gazelle now retains compiler-observed TypeScript and JavaScript imports from directories without a TypeScript target, alongside the existing JSON inputs. It follows their transitive imports without crossing existing target or generated-output owners, and leaves source exports and visibility unchanged. Repeated generation no longer reports retained direct sources as dropped.
