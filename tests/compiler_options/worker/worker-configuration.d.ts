// Shaped like the file `wrangler types` writes for a Cloudflare Worker: the
// project's Env plus the runtime's own globals, several of which are names the
// browser libs also declare.
interface KVNamespace {
  get(key: string): Promise<string | null>;
}

interface Env {
  SESSIONS: KVNamespace;
}

interface WorkerResponse {
  readonly status: number;
}

interface WorkerRequest {
  readonly url: string;
}

declare var Response: {
  new (body?: string | null, init?: { status?: number }): WorkerResponse;
};

// The Workers runtime's cache API. lib.dom and lib.webworker both declare
// `caches` as a CacheStorage, which has no `default`, and a lib declaration
// wins over an ambient one.
interface WorkerCacheStorage {
  readonly default: {
    match(request: WorkerRequest): Promise<WorkerResponse | undefined>;
  };
}

declare var caches: WorkerCacheStorage;

interface ExportedHandler<E = unknown> {
  fetch(request: WorkerRequest, env: E): Promise<WorkerResponse>;
}
