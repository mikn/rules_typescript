### Fixed

- **A `config`'s `setupFiles` entry loads under a DOM environment.** vitest
  resolves each entry through Node's resolver, which follows the runfiles link
  to the compiled file in `bazel-out`; `jsdom` and `happy-dom` load setup files
  through Vite, which serves the root and refused the path: `Cannot find module
  '/@fs/<bazel-out path>/vitest.setup.js'`. Layer 1 gives every file the
  runfiles hold its runfiles path as its id, the setup file with them, so it
  loads the way a test file does.
