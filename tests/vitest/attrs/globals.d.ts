// globals = True removes the need to import these, but nothing teaches the
// compiler about them: the generated tsconfig has no `types` knob (GAP-02).
declare function describe(name: string, fn: () => void): void;
declare function it(name: string, fn: () => void): void;
declare function expect(actual: unknown): { toBe(expected: unknown): void };
declare var __rulesTsAttrsSetup: string;
