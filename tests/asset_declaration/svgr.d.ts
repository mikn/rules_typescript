type SvgComponent = (props: { className?: string }) => null;
type TextAsset = { readonly words: number };

declare module "*.svg" {
  const component: SvgComponent;
  export default component;
}

declare module "*.txt" {
  const text: TextAsset;
  export default text;
}
