// JsRpcPromise is declared by @cloudflare/workers-types/experimental and by no
// other entry of the package.
export function isRpcPromise(value: unknown): value is JsRpcPromise {
  return value instanceof JsRpcPromise;
}
