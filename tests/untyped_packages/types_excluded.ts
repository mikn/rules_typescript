// Excluding `@types/ms` rather than `ms`: the runtime package stays, and its
// `paths` key has to stop redirecting into the declarations that just left.
// Nothing imports ms here -- the key it resolves through is the assertion.
export const value = 1;
