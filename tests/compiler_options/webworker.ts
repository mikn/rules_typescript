// DedicatedWorkerGlobalScope is declared only in lib.webworker, which no
// `target` implies.
export function boot(scope: DedicatedWorkerGlobalScope): void {
  scope.postMessage("ready");
}
