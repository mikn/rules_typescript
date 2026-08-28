/**
 * Every export carries an explicit type, so oxc can emit declarations from
 * syntax alone. This half of the workspace must build.
 */

export function add(a: number, b: number): number {
  return a + b;
}

export const ORIGIN: { x: number; y: number } = { x: 0, y: 0 };

export const NAME_PATTERN: RegExp = /^[a-z][a-z0-9_]*$/;
