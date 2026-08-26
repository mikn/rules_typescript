// Emits the typed .d.ts of a CSS Modules file.
//
//   node css_module_dts.mjs <input.module.css> <out.module.css.d.ts>
//
// Only class names that CSS Modules actually scopes locally are declared: the
// ones appearing in a selector. Declaration values (url(logo.png), font: 1.5em),
// comments, strings, at-rule preludes, @keyframes bodies and :global groups are
// not selectors, so nothing is taken from them. :local groups are the explicit
// form of the default scoping, so their class names are declared.
import { readFileSync, writeFileSync } from "node:fs";

const [, , cssPath, dtsPath] = process.argv;

// At-rules whose block contains style rules rather than declarations.
const NESTED_AT_RULES = new Set([
  "media",
  "supports",
  "layer",
  "container",
  "scope",
  "document",
  "starting-style",
]);

const IDENT_START = /[A-Za-z_]/;
const IDENT_CHAR = /[A-Za-z0-9_-]/;

const SCOPE_GROUP = /^:(global|local)\s*\(/i;

// The combinator form, with no parentheses: everything after it in the selector
// is scoped the other way. postcss-modules rejects a comma list whose selectors
// end up in different modes, so one mode per rule is all that has to be tracked.
const SCOPE_COMBINATOR = /^:(global|local)(?![\w-(])/i;

// Selector text with everything that cannot hold a local class name blanked
// out: strings, bracketed attribute tests, and anything unscoped. :global(...)
// turns scoping off for its group, :local(...) back on, and they nest.
function blankNonSelectorText(prelude, scoped) {
  let out = "";
  let i = 0;
  while (i < prelude.length) {
    const c = prelude[i];
    if (c === '"' || c === "'") {
      const end = skipString(prelude, i);
      out += " ".repeat(end - i);
      i = end;
    } else if (c === "[") {
      const end = skipBalanced(prelude, i, "[", "]");
      out += " ".repeat(end - i);
      i = end;
    } else if (c === ":" && SCOPE_GROUP.test(prelude.slice(i))) {
      const kind = SCOPE_GROUP.exec(prelude.slice(i))[1].toLowerCase();
      const open = prelude.indexOf("(", i);
      const end = skipBalanced(prelude, open, "(", ")");
      const close = prelude[end - 1] === ")" ? end - 1 : end;
      out += " ".repeat(open + 1 - i);
      out += blankNonSelectorText(prelude.slice(open + 1, close), kind === "local");
      out += " ".repeat(end - close);
      i = end;
    } else if (c === ":" && SCOPE_COMBINATOR.test(prelude.slice(i))) {
      const match = SCOPE_COMBINATOR.exec(prelude.slice(i));
      scoped = match[1].toLowerCase() === "local";
      out += " ".repeat(match[0].length);
      i += match[0].length;
    } else {
      out += scoped ? c : " ";
      i += 1;
    }
  }
  return out;
}

function skipString(text, i) {
  const quote = text[i];
  let j = i + 1;
  while (j < text.length) {
    if (text[j] === "\\") {
      j += 2;
      continue;
    }
    if (text[j] === quote) return j + 1;
    j += 1;
  }
  return text.length;
}

function skipBalanced(text, i, open, close) {
  let depth = 0;
  let j = i;
  while (j < text.length) {
    const c = text[j];
    if (c === "\\") {
      j += 2;
      continue;
    }
    if (c === '"' || c === "'") {
      j = skipString(text, j);
      continue;
    }
    if (c === open) depth += 1;
    else if (c === close) {
      depth -= 1;
      if (depth === 0) return j + 1;
    }
    j += 1;
  }
  return text.length;
}

function collectClasses(prelude, out, scoped) {
  const text = blankNonSelectorText(prelude, scoped);
  let i = 0;
  while (i < text.length) {
    if (text[i] === "\\") {
      i += 2;
      continue;
    }
    if (text[i] !== ".") {
      i += 1;
      continue;
    }
    let j = i + 1;
    if (j < text.length && text[j] === "-") j += 1;
    if (j >= text.length || !IDENT_START.test(text[j])) {
      i += 1;
      continue;
    }
    while (j < text.length && IDENT_CHAR.test(text[j])) j += 1;
    out.add(text.slice(i + 1, j));
    i = j;
  }
}

// Walks one block (or the stylesheet, at depth 0) collecting class names from
// every selector prelude it contains. Returns the index just past the block.
function walk(src, start, out, scoped) {
  let prelude = "";
  let i = start;
  while (i < src.length) {
    const c = src[i];
    if (c === "/" && src[i + 1] === "*") {
      const end = src.indexOf("*/", i + 2);
      i = end === -1 ? src.length : end + 2;
      continue;
    }
    if (c === '"' || c === "'") {
      const end = skipString(src, i);
      prelude += src.slice(i, end);
      i = end;
      continue;
    }
    if (c === "{") {
      const trimmed = prelude.trim();
      const scopeBlock = /^:(global|local)$/i.exec(trimmed);
      const name = /^@([A-Za-z-]+)/.exec(trimmed);
      if (scopeBlock !== null) {
        i = walk(src, i + 1, out, scopeBlock[1].toLowerCase() === "local");
      } else if (name === null) {
        collectClasses(prelude, out, scoped);
        i = walk(src, i + 1, out, scoped);
      } else if (NESTED_AT_RULES.has(name[1].toLowerCase())) {
        i = walk(src, i + 1, out, scoped);
      } else {
        i = skipBalanced(src, i, "{", "}");
      }
      prelude = "";
      continue;
    }
    if (c === ";") {
      prelude = "";
      i += 1;
      continue;
    }
    if (c === "}") {
      return i + 1;
    }
    prelude += c;
    i += 1;
  }
  return i;
}

const classes = new Set();
walk(readFileSync(cssPath, "utf8"), 0, classes, true);

const isBareIdent = (name) => /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(name);
const fields = [...classes].map(
  (name) => `  readonly ${isBareIdent(name) ? name : JSON.stringify(name)}: string;`,
);

writeFileSync(
  dtsPath,
  ["declare const styles: {", ...fields, "};", "export default styles;", ""].join("\n"),
  "utf8",
);
