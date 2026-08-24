import defaultLeaf, * as leafNs from "./leaf";

export * as reexportedLeaf from "./leaf";

// "./hidden" quoted in every position that is NOT an import. ./hidden reaches
// this target through ./leaf, so recognising any of these as an import fails
// the build -- which is the assertion.
const objectKey = { from: "./hidden" };
const template = `import { x } from "./hidden"`;
const inRegex = /from "\.\/hidden"/;
const thrown = () => new Error('import "./hidden"');
const divided = (a: number, b: number): number => a / b;

export const forms = {
  id: defaultLeaf.id,
  exported: Object.keys(leafNs).length,
  objectKey: objectKey.from,
  template,
  matched: inRegex.source,
  thrown: thrown().message,
  divided: divided(4, 2),
};
