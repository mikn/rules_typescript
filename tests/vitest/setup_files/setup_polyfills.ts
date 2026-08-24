globalThis.__rulesTsMatchMedia = (query: string) => ({
  matches: query.includes("min-width"),
});

globalThis.__rulesTsSetupOrder = ["polyfills"];
