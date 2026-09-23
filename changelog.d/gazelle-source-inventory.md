### Added

Gazelle's `//gazelle/sources:gazelle` operation refreshes existing canonical
TypeScript compile source inventories without activating new targets or changing
dependencies and emission. Nested compiler inputs remain visible when Gazelle
collects files under `generation_mode update_only`.
