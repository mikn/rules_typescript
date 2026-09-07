// Implicit any: only legal because the tsconfig baseline turns strict off.
export function widen(value) {
  return value;
}

// Promise and DedicatedWorkerGlobalScope: only legal because the leaf tsconfig's
// `lib` overrides the ["es5"] its base asks for.
export function boot(scope: DedicatedWorkerGlobalScope): Promise<void> {
  scope.postMessage("ready");
  return Promise.resolve();
}
