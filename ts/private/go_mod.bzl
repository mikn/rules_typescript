"""The modules a go.mod pins, and the sums its go.sum carries for them.

`ts/private/tsgo_source/go.mod` names the compiler's main package in a `tool`
directive and, after `go mod tidy`, requires its module and every module that
build reaches; the `ts` extension declares one `go_repository` per require
from these readers (docs/rules/providers.md § tsgo from source).

Pure text -> struct, so //tests/toolchain:go_mod_tests pins every message
without a fetch; the extension turns the error into a fail().
"""

def parse_go_mod(content):
    """The `tool` and `require` directives of a go.mod, single or blocked.

    Args:
        content: The text of go.mod.

    Returns:
        struct(tools = [package path], requires = {module path: version}).
    """
    tools = []
    requires = {}
    block = None
    for raw in content.splitlines():
        tokens = _tokens(raw.split("//", 1)[0])
        if not tokens:
            continue
        if block != None:
            if tokens == [")"]:
                block = None
            else:
                _directive(block, tokens, tools, requires)
        elif len(tokens) == 2 and tokens[1] == "(":
            block = tokens[0]
        else:
            _directive(tokens[0], tokens[1:], tools, requires)
    return struct(tools = tools, requires = requires)

def _tokens(line):
    return [t for t in line.replace("\t", " ").split(" ") if t]

def _directive(name, tokens, tools, requires):
    if name == "tool" and len(tokens) == 1:
        tools.append(tokens[0])
    elif name == "require" and len(tokens) == 2:
        requires[tokens[0]] = tokens[1]

def parse_go_sum(content):
    """The module zip sums of a go.sum.

    Args:
        content: The text of go.sum.

    Returns:
        {(module path, version): "h1:..."}; a `/go.mod` line is the sum of a
        go.mod file, not of a module, and is left out.
    """
    sums = {}
    for line in content.splitlines():
        tokens = _tokens(line)
        if len(tokens) == 3 and not tokens[1].endswith("/go.mod"):
            sums[(tokens[0], tokens[1])] = tokens[2]
    return sums

def go_repo_name(importpath):
    """The repository name Gazelle writes for a module.

    Args:
        importpath: A module path, `github.com/zeebo/xxh3`.

    Returns:
        The host's labels reversed, then the path, joined by `_` and lowercased
        with every other character replaced by `_`: `com_github_zeebo_xxh3`.
    """
    segments = importpath.split("/")
    parts = reversed(segments[0].split(".")) + segments[1:]
    joined = "_".join(parts)
    return "".join([c.lower() if c.isalnum() else "_" for c in joined.elems()])

def _error(message):
    return struct(tool = None, modules = [], error = message)

def tsgo_source_modules(go_mod, go_sum, go_mod_name):
    """The compiler's module graph: what a tool-only go.mod and its go.sum pin.

    Args:
        go_mod: The text of the go.mod whose one `tool` directive names the
            compiler's main package.
        go_sum: The text of the go.sum beside it.
        go_mod_name: How to name the go.mod in a message.

    Returns:
        struct(tool = struct(package, module, version), modules =
        [struct(path, version, sum)], error). `error` is "" on success and the
        only field to read otherwise.
    """
    parsed = parse_go_mod(go_mod)
    if len(parsed.tools) != 1:
        return _error((
            "{} names {} tool directives; one names the compiler's main " +
            "package, `tool github.com/microsoft/typescript-go/cmd/tsgo`."
        ).format(go_mod_name, len(parsed.tools)))
    package = parsed.tools[0]
    module = ""
    for path in parsed.requires:
        if package == path or package.startswith(path + "/"):
            if len(path) > len(module):
                module = path
    if not module:
        return _error((
            "{} requires no module holding its tool {}; run `go mod tidy` " +
            "in its directory."
        ).format(go_mod_name, package))

    sums = parse_go_sum(go_sum)
    modules = []
    for path, version in parsed.requires.items():
        sum = sums.get((path, version))
        if not sum:
            return _error((
                "go.sum beside {} has no h1 sum for {} {}; run `go mod tidy` " +
                "in its directory."
            ).format(go_mod_name, path, version))
        modules.append(struct(path = path, version = version, sum = sum))
    return struct(
        tool = struct(
            package = package,
            module = module,
            version = parsed.requires[module],
        ),
        modules = modules,
        error = "",
    )
