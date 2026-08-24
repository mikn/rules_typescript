// Implicit any: only legal because the tsconfig baseline turns strict off.
export function widen(value) {
  return value;
}

// Promise and DedicatedWorkerGlobalScope: only legal because the target's own
// `lib` overrides the ["es5"] the tsconfig baseline asks for.
export function boot(scope: DedicatedWorkerGlobalScope): Promise<void> {
  scope.postMessage("ready");
  return Promise.resolve();
}
