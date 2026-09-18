export interface Node {
  type: string;
  props: Record<string, unknown>;
}

export namespace JSX {
  export type Element = Node;
  export interface IntrinsicElements {
    p: { id?: string; children?: unknown };
  }
}

export const Fragment = "fragment";

export function jsx(type: string, props: Record<string, unknown>): Node {
  return { type, props };
}

export const jsxs = jsx;
