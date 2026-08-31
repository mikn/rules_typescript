// Compiles only if the resolver tsgo ends up with is the one `module: NodeNext`
// demands: a baseline that also asserted moduleResolution would leave Bundler
// beside it, which is TS5109 before the program is even read.
export function port(value: number): string {
  return `:${value}`;
}
