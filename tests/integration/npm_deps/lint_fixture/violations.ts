// A real oxlint violation: the no-var rule in lint_fixture/oxlint.json rejects `var`.
// Nothing in the workspace builds this target; the test asserts the build FAILS.
var counter = 1;

export { counter };
