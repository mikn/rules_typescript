// Ambient stand-ins for the browser globals a real setup file polyfills
// (matchMedia, ResizeObserver, ...).  Declared locally so this test does not
// depend on which `lib` ts_compile defaults to.
declare var __rulesTsMatchMedia: (query: string) => { matches: boolean };
declare var __rulesTsSetupOrder: string[];
