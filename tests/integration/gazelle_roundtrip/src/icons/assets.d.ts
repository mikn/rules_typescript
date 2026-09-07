declare module "*.svg" {
  const asset: { readonly viewBox: string };
  export default asset;
}
