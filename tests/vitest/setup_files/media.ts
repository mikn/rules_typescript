export function prefersWide(): boolean {
  return globalThis.__rulesTsMatchMedia("(min-width: 600px)").matches;
}

export function setupOrder(): string[] {
  return globalThis.__rulesTsSetupOrder;
}
