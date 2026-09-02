// @cloudflare/workers-types as a DIRECT dep, which is the other route into a
// program: it ships declarations and no runtime module, so ts_compile names its
// entry point in the tsconfig `files` array and every global in it is in scope
// with no import anywhere. This is the shape //web:worker_entry wants.
export function target(fetcher: Fetcher): Fetcher {
  return fetcher;
}
