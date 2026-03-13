/**
 * Minimal JSX type declarations for testing without @types/react.
 */

declare namespace JSX {
  type Element = {};
  interface IntrinsicElements {
    [elemName: string]: any;
  }
}

declare module "react/jsx-runtime" {
  export function jsx(type: any, props: any, key?: string): JSX.Element;
  export function jsxs(type: any, props: any, key?: string): JSX.Element;
}
