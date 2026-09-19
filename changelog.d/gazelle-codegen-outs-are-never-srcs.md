### Fixed

- **Gazelle no longer lists a `ts_codegen`'s declared `outs` as sources.** A
  local `wrangler types` leaves `worker-configuration.d.ts` on disk beside the
  `ts_codegen` that declares it in `outs`; the program lists it, and Gazelle
  wrote it into the package's `ts_compile.srcs` and `ts_test.srcs`, so the
  BUILD file depended on whether the checkout had run the generator. A file a
  `ts_codegen` declares in `outs` is that target's output, on disk or not, as
  a file under its `out_dir` already was: no rule lists it as a src, and the
  program reaching it depends on the codegen.
