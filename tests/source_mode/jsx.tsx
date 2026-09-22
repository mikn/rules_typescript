declare global {
  namespace JSX {
    interface IntrinsicElements {
      span: object;
    }
  }
}

export const view = <span />;
