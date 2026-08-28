export namespace JSX {
  interface IntrinsicElements {
    div: { id?: string };
  }
  type Element = string;
}

export declare function jsx(type: string, props: unknown): string;
export declare function jsxs(type: string, props: unknown): string;
export declare const Fragment: unique symbol;
