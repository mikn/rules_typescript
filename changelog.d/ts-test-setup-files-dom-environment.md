### Fixed

- **A `config`'s `setupFiles` entry loads under a DOM environment.** vitest
  resolves each entry through Node's resolver, which follows the runfiles link
  to the compiled file in `bazel-out`; `jsdom` and `happy-dom` load setup files
  through Vite, which serves the root and refused the path: `Cannot find module
  '/@fs/<bazel-out path>/vitest.setup.js'`. Layer 1 now carries a plugin that
  answers that request with the staged path, so the setup file loads the way a
  test file does.
