### Changed — ts_compile

- **The strict-deps check reads tsgo's own listing.** The one tsgo action a
  target runs -- `TsgoDeclare`, or `TsgoCheck` under
  `--//ts:declarations=oxc` -- runs with `--explainFiles`, and tsaction reads
  every edge it lists against an ownership manifest the rule writes beside it:
  the target's own srcs, each first-party target in the closure with the files
  it stages, and the forest's packages split into the ones `deps` declare and
  the rest. An `Imported via`, `Referenced via` or `Type library referenced
  via` edge from one of the target's files into a file whose owner is not in
  `deps` fails the action, naming the file, the specifier, the file it
  resolved to and the label to add. Edges the source scanner never saw now
  fail: a `paths` alias into another target, a `/// <reference path>`, a
  `/// <reference types>` into a package only a dep's closure carries, the
  JSX runtime import tsgo adds to every `.tsx`. A direct package's
  `@types/<name>` twin, which the forest links for it, counts as declared.
  The message reads `add //pkg:target to deps`, without quotes.
