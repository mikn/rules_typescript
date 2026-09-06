"""A workspace member's package.json as the compiled package's.

A member's manifest names its sources: `"exports": {".": "./src/index.ts"}`.
The hub's view of the member links that manifest into node_modules beside the
files ts_compile emits, so every source-file target is rewritten to the emitted
one -- `main`, `module`, `browser`, `exports` and `imports` to the `.js`, `types`,
`typings` and a `types` condition under `exports` to the `.d.ts`. tsc maps a
`.js` target to the `.d.ts` beside it and node runs the `.js`, so one manifest
serves the type-check and the runtime. A member that sets no `type` is ESM: the
emitted `.js` is.

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

_EMITTED = {
    "js": {".tsx": ".js", ".ts": ".js", ".mts": ".mjs", ".cts": ".cjs"},
    "dts": {".tsx": ".d.ts", ".ts": ".d.ts", ".mts": ".d.mts", ".cts": ".d.cts"},
}

_DECLARATION_EXTENSIONS = (".d.ts", ".d.mts", ".d.cts")

# Starlark has no recursion or while loop; a manifest is a few hundred tokens.
_TOKEN_BUDGET = 1 << 20

def _emitted(target, role):
    if role not in _EMITTED or target.endswith(_DECLARATION_EXTENSIONS):
        return target
    for source, emitted in _EMITTED[role].items():
        if target.endswith(source):
            return target[:-len(source)] + emitted
    return target

def _child_role(role, key):
    if role == "root":
        return _ROLE_OF_FIELD.get(key, "keep")
    if role == "js" and key == "types":
        return "dts"
    return role

def _encode(manifest):
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
                emit.append(struct(value = value[key], role = _child_role(item.role, key)))
            emit.append(struct(text = "}"))
        elif type(value) == "list":
            emit = [struct(text = "[")]
            for i, element in enumerate(value):
                if i:
                    emit.append(struct(text = ","))
                emit.append(struct(value = element, role = item.role))
            emit.append(struct(text = "]"))
        elif type(value) == "string":
            emit = [struct(text = json.encode(_emitted(value, item.role)))]
        else:
            emit = [struct(text = json.encode(value))]
        stack.extend(reversed(emit))
    if stack:
        fail("member_manifest: a package.json with more than {} tokens".format(_TOKEN_BUDGET))
    return "".join(out)

def member_manifest_json(manifest):
    """The manifest's JSON with source-file targets rewritten, or None without a `name`.

    Args:
        manifest: A member's decoded package.json.

    Returns:
        The JSON text the view writes as the member's package.json, or None.
    """
    if type(manifest) != "dict":
        return None
    name = manifest.get("name")
    if type(name) != "string" or not name:
        return None
    out = dict(manifest)
    out.setdefault("type", "module")
    return _encode(out)
