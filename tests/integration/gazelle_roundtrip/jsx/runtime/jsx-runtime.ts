export declare namespace JSX {
  interface IntrinsicElements {
    div: { id?: string };
  }
  type Element = string;
}

export function jsx(type: string, props: unknown): string {
  return type + String(props);
}

export const jsxs = jsx;
