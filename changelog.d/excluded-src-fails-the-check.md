### Changed

- **A src the chain's `exclude` names and the program never read fails
  `TsgoCheck`.** `include` names each src no root of the chain names, and tsc
  applies the chain's `exclude` to what `include` names, so such a src left
  the program without a word: unchecked, its `.d.ts` never written. Now
  `tsaction tsgo` reads the chain (`-tsconfig=FILE`, beside `-check`) and
  fails naming the src and the entry; a src an import brings in is in the
  program and passes. The program is the srcs: the file leaves `srcs` or the
  entry leaves `exclude`.
