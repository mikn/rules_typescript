/**
 * Minimal JSX type declarations for tests without @types/react.
 *
 * Provides enough type surface for tsgo to type-check JSX elements
 * and the react-jsx automatic runtime transform. Once npm support
 * (ts_npm_package + @types/react) is wired up, this shim can be removed.
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
