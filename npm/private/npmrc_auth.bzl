"""The credentials an .npmrc grants a tarball URL, for a repository rule's fetch.

A leaf on purpose: npm_import.bzl and ts/private/toolchain.bzl both load it, and
toolchain.bzl sits under ts_compile.bzl, which workspace_package.bzl -- a dep of
npm_import's bzl_library -- loads.
"""

def _expand_env(value, getenv):
    """Substitutes ${VAR} from the fetch environment, the way npm does.

    This is the supported way to keep a token out of the workspace: the .npmrc
    that Bazel tracks names an environment variable, and the value only ever
    exists inside one fetch. rctx.getenv registers the variable, so changing it
    refetches.
    """
    out = ""
    rest = value
    for _ in range(16):
        start = rest.find("${")
        if start == -1:
            break
        end = rest.find("}", start)
        if end == -1:
            break
        out += rest[:start] + getenv(rest[start + 2:end], "")
        rest = rest[end + 1:]
    return out + rest

def _auth_scope(url):
    """The `//host/path/` form npm keys credentials by, for a URL or an .npmrc key."""
    for scheme in ("https://", "http://"):
        if url.startswith(scheme):
            url = url[len(scheme):]
            break
    if not url.startswith("//"):
        url = "//" + url
    return url if url.endswith("/") else url + "/"

def npmrc_auth_fields(content, url):
    """The credential fields of the .npmrc scope that best matches a URL.

    npm keys credentials by `//host/path/` and applies the LONGEST matching
    prefix, so a registry mounted on a path (an Artifactory repo, say) carries its
    own token without claiming the whole host.

    Args:
        content: The .npmrc text.
        url: The tarball URL about to be fetched.

    Returns:
        (scope, {field: raw_value}) -- raw because ${VAR} is expanded by the
        caller, which is the only place the environment is available.
    """
    request_scope = _auth_scope(url)
    best_scope = ""
    fields = {}
    for raw in content.split("\n"):
        line = raw.strip()
        if not line or line.startswith("#") or line.startswith(";") or line.startswith("["):
            continue
        key, sep, value = line.partition("=")
        if not sep or not key.strip().startswith("//"):
            continue
        scope, _, field = key.strip().rpartition(":")
        scope = _auth_scope(scope)
        if not request_scope.startswith(scope) or len(scope) < len(best_scope):
            continue
        if scope != best_scope:
            best_scope = scope
            fields = {}
        fields[field] = value.strip().strip("'\"")
    return (best_scope, fields)

def npmrc_auth(content, url, getenv, npmrc_label = ".npmrc"):
    """The `auth` dict rctx.download needs for one tarball URL, or {}.

    Built at fetch time, never in the module extension: an extension's result is
    serialised into MODULE.bazel.lock, a committed file, and a repository rule's
    attribute values go into it verbatim. What this repository records is the
    .npmrc's label; the token exists only for the duration of the fetch.

    Args:
        content: The .npmrc text.
        url: The tarball URL about to be fetched.
        getenv: rctx.getenv, which registers each ${VAR} the .npmrc names.
        npmrc_label: How the .npmrc is named in an error.

    Returns:
        {url: auth_spec} for rctx.download's `auth`, or {} when the .npmrc
        grants the URL nothing.
    """
    scope, fields = npmrc_auth_fields(content, url)

    token = _expand_env(fields.get("_authToken", ""), getenv)
    if token:
        return {url: {"type": "pattern", "pattern": "Bearer <password>", "login": "", "password": token}}

    basic = _expand_env(fields.get("_auth", ""), getenv)
    if basic:
        return {url: {"type": "pattern", "pattern": "Basic <password>", "login": "", "password": basic}}

    if "_password" in fields:
        fail(
            "npm: {} configures `username`/`_password` for {}, which this ruleset ".format(
                npmrc_label,
                scope,
            ) +
            "cannot use: npm stores `_password` base64-encoded and Starlark has no way " +
            "to decode it (there is no chr()). Use `_authToken=` or `_auth=` instead -- " +
            "`npm config set //host/:_authToken` writes the first, and `_auth` is the " +
            "same base64 blob you already have.",
        )
    return {}
