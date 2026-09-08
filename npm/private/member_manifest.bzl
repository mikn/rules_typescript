"""A workspace member's package.json as the compiled package's.

A member's manifest names its sources: `"exports": {".": "./src/index.ts"}`.
The hub's view of the member links that manifest into node_modules beside the
files ts_compile emits, so every source-file target is rewritten to the emitted
one -- `main`, `module`, `browser`, `exports` and `imports` to the `.js`, or to
the `.jsx` for a `.tsx` under `jsx: preserve`; `types`, `typings` and a `types`
condition under `exports` to the `.d.ts`. tsc maps a `.js` or `.jsx` target to
the `.d.ts` beside it and node runs the `.js`, so one manifest serves the
type-check and the runtime. A member that sets no `type` is ESM: the emitted
`.js` is.

The view rewrites at analysis, where the compiling target's declared jsx is
known; the module extension that reads the file passes its text through.

The text is encoded here, key order kept: an `exports` condition map is read in
the order it is written, and json.encode sorts keys.
"""

_ROLE_OF_FIELD = {
    "main": "js",
    "module": "js",
    "browser": "js",
    "exports": "js",
    "imports": "js",
    "types": "dts",
    "typings": "dts",
}

_DECLARATION_EXTENSIONS = (".d.ts", ".d.mts", ".d.cts")

# Starlark has no recursion or while loop; a manifest is a few hundred tokens.
_TOKEN_BUDGET = 1 << 20

def _emitted_by_role(jsx):
    tsx = ".jsx" if jsx == "preserve" else ".js"
    return {
        "js": {".tsx": tsx, ".ts": ".js", ".mts": ".mjs", ".cts": ".cjs"},
        "dts": {
            ".tsx": ".d.ts",
            ".ts": ".d.ts",
            ".mts": ".d.mts",
            ".cts": ".d.cts",
        },
    }

def _emitted(target, role, by_role):
    if role not in by_role or target.endswith(_DECLARATION_EXTENSIONS):
        return target
    for source, emitted in by_role[role].items():
        if target.endswith(source):
            return target[:-len(source)] + emitted
    return target

def _child_role(role, key):
    if role == "root":
        return _ROLE_OF_FIELD.get(key, "keep")
    if role == "js" and key == "types":
        return "dts"
    return role

def _encode(manifest, jsx):
    by_role = _emitted_by_role(jsx)
    stack = [struct(value = manifest, role = "root")]
    out = []
    for _ in range(_TOKEN_BUDGET):
        if not stack:
            break
        item = stack.pop()
        if hasattr(item, "text"):
            out.append(item.text)
            continue
        value = item.value
        if type(value) == "dict":
            emit = [struct(text = "{")]
            for i, key in enumerate(value.keys()):
                if i:
                    emit.append(struct(text = ","))
                emit.append(struct(text = json.encode(key) + ":"))
                emit.append(struct(
                    value = value[key],
                    role = _child_role(item.role, key),
                ))
            emit.append(struct(text = "}"))
        elif type(value) == "list":
            emit = [struct(text = "[")]
            for i, element in enumerate(value):
                if i:
                    emit.append(struct(text = ","))
                emit.append(struct(value = element, role = item.role))
            emit.append(struct(text = "]"))
        elif type(value) == "string":
            text = json.encode(_emitted(value, item.role, by_role))
            emit = [struct(text = text)]
        else:
            emit = [struct(text = json.encode(value))]
        stack.extend(reversed(emit))
    if stack:
        fail("member_manifest: a package.json with more than {} tokens".format(
            _TOKEN_BUDGET,
        ))
    return "".join(out)

def member_manifest_json(manifest, jsx):
    """The manifest's JSON, each source target rewritten to the emitted file.

    Args:
        manifest: A member's decoded package.json, a dict with a `name`.
        jsx: The compiling target's declared jsx, `TsConfigInfo.jsx`: a `.tsx`
            target names the `.jsx` under `"preserve"` and the `.js` otherwise.

    Returns:
        The JSON text the view writes as the member's package.json.
    """
    out = dict(manifest)
    out.setdefault("type", "module")
    return _encode(out, jsx)
