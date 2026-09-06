#!/usr/bin/env python3
"""Stages the two programs bench_forest.sh times and compares their output.

Both replicas hold every non-npm input of the target's TsgoDeclare action at its
exec-root path, as linux-sandbox stages them. The paths replica adds the npm
inputs the same way and runs the action's own tsconfig. The forest replica
instead holds a node_modules tree laid out as node_modules.bzl lays out a ts_test
runtime tree -- the target's declared resolution of each name at the top level,
every other resolution under .pnpm/ and linked from the packages that asked for
it, each workspace member under its link: name with its manifest's source-file
targets rewritten to the emitted extensions -- and runs a tsconfig with no npm
`paths`, `typeRoots` pinned to the forest and `types` as the user's tsconfig
chain sets it.

Subcommands:
  members --aquery A --deps-xml X --execroot E --label L
      print the workspace-member views among the target's deps: view, target
  build --aquery A --deps-xml X --members-xml M --workspace W --execroot E \
        --label L --paths DIR --forest DIR --plan OUT.json [--member-forest-root DIR]
      stage both replicas, write the tsconfigs, write the plan
  compare --plan P --forest-check F --paths-check Q [--forest-explain FE \
          --paths-explain PE] [--package NAME ...]
      compare diagnostics and programs; exit 1 when the diagnostics differ
"""

import argparse
import json
import os
import re
import shutil
import sys
import xml.etree.ElementTree as ET

DECLARATION_SUFFIXES = (".d.ts", ".d.mts", ".d.cts")
SOURCE_TO_JS = {".tsx": ".js", ".ts": ".js", ".mts": ".mjs", ".cts": ".cjs"}
SOURCE_TO_DTS = {".tsx": ".d.ts", ".ts": ".d.ts", ".mts": ".d.mts", ".cts": ".d.cts"}
PROGRAM_SUFFIXES = (".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".json")


def is_declaration(path):
    return path.endswith(DECLARATION_SUFFIXES)


# ─── aquery ───────────────────────────────────────────────────────────────────


def read_aquery(path):
    data = json.load(open(path))
    actions = data.get("actions", [])
    if len(actions) != 1:
        sys.exit("bench_forest: expected one TsgoDeclare action, aquery lists {}".format(len(actions)))
    action = actions[0]
    artifacts = {a["id"]: a for a in data.get("artifacts", [])}
    fragments = {f["id"]: f for f in data.get("pathFragments", [])}

    def artifact_path(fid):
        parts = []
        while fid is not None:
            fragment = fragments[fid]
            parts.append(fragment["label"])
            fid = fragment.get("parentId")
        return "/".join(reversed(parts))

    depsets = {s["id"]: s for s in data.get("depSetOfFiles", [])}
    memo = {}

    def flatten(sid):
        if sid in memo:
            return memo[sid]
        depset = depsets[sid]
        out = set(depset.get("directArtifactIds", []))
        for tid in depset.get("transitiveDepSetIds", []):
            out |= flatten(tid)
        memo[sid] = out
        return out

    ids = set()
    for sid in action.get("inputDepSetIds", []):
        ids |= flatten(sid)
    inputs = sorted(artifact_path(artifacts[i]["pathFragmentId"]) for i in ids)
    outputs = sorted(artifact_path(artifacts[o]["pathFragmentId"]) for o in action.get("outputIds", []))
    arguments = action.get("arguments", [])
    if len(arguments) < 3 or arguments[1] != "--project":
        sys.exit("bench_forest: unexpected TsgoDeclare arguments {}".format(arguments))
    return {"tsc": arguments[0], "tsconfig": arguments[2], "inputs": inputs, "outputs": outputs}


# ─── labels ───────────────────────────────────────────────────────────────────


def canonical(label):
    if label.startswith("@@"):
        label = label[2:]
    elif label.startswith("@"):
        label = label[1:]
    if "//" in label and ":" not in label.split("//", 1)[1]:
        label = label + ":" + label.rsplit("/", 1)[-1]
    return label


def label_repo_and_name(label):
    repo, _, rest = canonical(label).partition("//")
    return repo, rest.split(":", 1)[1]


def package_dir_of(label):
    return canonical(label).split("//", 1)[1].split(":", 1)[0]


# ─── npm packages ─────────────────────────────────────────────────────────────


class NpmPackage:
    """One ts_npm_package stanza: a package, or an npm alias of one under its second name."""

    def __init__(self, repo, prefix, text):
        self.repo = repo
        self.prefix = prefix
        self.target = re.search(r'name = "([^"]+)"', text).group(1)
        self.name = re.search(r'package_name = "([^"]+)"', text).group(1)
        self.version = re.search(r'package_version = "([^"]+)"', text).group(1)
        peer = re.search(r'peer_id = "([^"]*)"', text)
        self.peer_id = peer.group(1) if peer else ""
        self.package_root = re.search(r'package_dir = ":(.+?)/package\.json"', text).group(1)
        deps = re.search(r"deps = \[(.*?)\]", text, re.S)
        self.dep_labels = [prefix + r + "//:" + t for r, t in re.findall(r'"@([^/"]+)//:([^"]+)"', deps.group(1))] if deps else []
        types_dep = re.search(r'types_dep = "@([^/"]+)//:([^"]+)"', text)
        self.types_dep_label = prefix + types_dep.group(1) + "//:" + types_dep.group(2) if types_dep else None

    @property
    def label(self):
        return "{}//:{}".format(self.repo, self.target)

    @property
    def key(self):
        key = "{}@{}".format(self.name, self.version)
        return key if not self.peer_id else "{}_{}".format(key, self.peer_id)

    def rank(self):
        # _version_rank plus _resolution_rank: version, then the un-suffixed
        # resolution ahead of a peer variant of the same version.
        numeric, _, prerelease = self.version.partition("-")
        parts = []
        for segment in numeric.split(".")[:3]:
            digits = ""
            for ch in segment:
                if not ch.isdigit():
                    break
                digits += ch
            parts.append(int(digits) if digits else 0)
        parts += [0] * (3 - len(parts))
        return tuple(parts) + (0 if prerelease else 1, prerelease, 0 if self.peer_id else 1)

    def store_dir(self):
        store = "{}@{}".format(self.name.replace("/", "+"), self.version)
        if self.peer_id:
            store += "_" + self.peer_id
        return store


class Store:
    """Every ts_npm_package stanza the execroot's npm repositories hold, read on demand."""

    def __init__(self, execroot, prefix):
        self.execroot = execroot
        self.prefix = prefix
        self.repos = {}

    def repo_packages(self, repo):
        if repo not in self.repos:
            build = os.path.join(self.execroot, "external", repo, "BUILD.bazel")
            text = open(build).read() if os.path.exists(build) else ""
            stanzas = re.findall(r"ts_npm_package\((.*?)\n\)", text, re.S)
            self.repos[repo] = {p.target: p for p in (NpmPackage(repo, self.prefix, s) for s in stanzas)}
        return self.repos[repo]

    def package(self, label):
        """The stanza a label names, or None when Bazel never fetched its repository.

        The execroot holds the union of every build's repositories, so a stanza
        can name a dep whose repository no analysed target ever needed.
        """
        repo, target = canonical(label).split("//:")
        return self.repo_packages(repo).get(target)

    def closure(self, root_labels):
        """Every package reachable from the roots through deps and the @types pairing.

        Returns:
            (dict label -> NpmPackage, list of "<from> -> <label>" edges to unextracted repositories)
        """
        seen = {}
        unextracted = []
        queue = [(None, label) for label in root_labels]
        while queue:
            origin, label = queue.pop()
            if label in seen:
                continue
            package = self.package(label)
            if package is None:
                unextracted.append("{} -> {}".format(origin, label))
                continue
            seen[label] = package
            queue.extend((package.key, dep) for dep in package.dep_labels)
            if package.types_dep_label:
                queue.append((package.key, package.types_dep_label))
        return seen, unextracted


def npm_prefix_and_repos(inputs, execroot):
    """The npm repository names among the inputs and their canonical prefix (`<module>++npm+`)."""
    repos = []
    seen = set()
    for path in inputs:
        if not path.startswith("external/"):
            continue
        repo = path.split("/")[1]
        if repo in seen:
            continue
        seen.add(repo)
        build = os.path.join(execroot, "external", repo, "BUILD.bazel")
        if os.path.exists(build) and "ts_npm_package(" in open(build).read():
            repos.append(repo)
    if not repos:
        sys.exit("bench_forest: no npm package repository among the action's inputs")
    return repos[0][: repos[0].index("npm__")], repos


class Hub:
    """The generated @npm hub: its aliases, per importer, and one view per workspace member."""

    ALIAS = re.compile(r'alias\(\s*name = "([^"]+)",\s*actual = "@([^/"]+)//:([^"]+)",')

    def __init__(self, execroot, prefix):
        self.execroot = execroot
        self.prefix = prefix
        self.repo = prefix + "npm"
        # `bazel query` prints the apparent name the consumer wrote, `@npm`; the
        # canonical one is what the execroot holds.
        self.apparent = self.repo.rsplit("+", 1)[-1]
        text = open(os.path.join(execroot, "external", self.repo, "BUILD.bazel")).read()
        self.aliases = self._aliases(text)
        self.importers = {}
        self.views = {}
        for name, package_name, target, member_dir in re.findall(
            r'npm_workspace_package\(\s*name = "([^"]+)",\s*package_name = "([^"]+)",\s*target = "([^"]+)",\s*member_dir = "([^"]+)",',
            text,
        ):
            self.views[name] = {"view": name, "package_name": package_name, "target": target, "member_dir": member_dir}

    def _aliases(self, text):
        return {name: "{}{}//:{}".format(self.prefix, repo, target) for name, repo, target in self.ALIAS.findall(text)}

    def importer_aliases(self, importer_dir):
        """The hub's aliases for one lockfile importer: the resolution that importer asked for (D7)."""
        if importer_dir not in self.importers:
            build = os.path.join(self.execroot, "external", self.repo, importer_dir, "BUILD.bazel")
            self.importers[importer_dir] = self._aliases(open(build).read()) if os.path.exists(build) else {}
        return self.importers[importer_dir]

    def resolve(self, name, importer_dir):
        """A hub name's package label as `importer_dir` resolves it, and the flat hub's answer."""
        flat = self.aliases.get(name)
        scoped = self.importer_aliases(importer_dir).get(name)
        return scoped or flat, flat


def rule_deps(xml_path, label):
    """The `deps` labels of one rule in a `bazel query --output=xml` document."""
    root = ET.parse(xml_path).getroot()
    for rule in root.iter("rule"):
        if canonical(rule.get("name")) != canonical(label):
            continue
        for lst in rule.iter("list"):
            if lst.get("name") == "deps":
                return [canonical(l.get("value")) for l in lst.iter("label")]
        return []
    sys.exit("bench_forest: {} is not a rule in {}".format(label, xml_path))


def classify_deps(dep_labels, hub, store, importer_dir):
    """Split a rule's deps into npm package labels, member views and everything else.

    A hub name resolves as the rule's own lockfile importer resolves it, which is
    the label C.4 writes; where the flat hub answers differently the pair is kept,
    and where Bazel never fetched the importer's answer the flat one stands.
    """
    npm, members, other, substituted = [], [], [], {}
    for label in dep_labels:
        repo, name = label_repo_and_name(label)
        if repo in (hub.repo, hub.apparent) and name in hub.views:
            members.append(hub.views[name])
        elif repo in (hub.repo, hub.apparent) and name in hub.aliases:
            resolved, flat = hub.resolve(name, importer_dir)
            if resolved != flat:
                extracted = store.package(resolved) is not None
                substituted[name] = {"importer": resolved, "flat": flat, "importer_extracted": extracted}
                if not extracted:
                    resolved = flat
            npm.append(resolved)
        elif repo.startswith(hub.prefix + "npm__") or (hub.prefix + repo).startswith(hub.prefix + "npm__"):
            npm.append(canonical(label) if repo.startswith(hub.prefix) else hub.prefix + canonical(label))
        else:
            other.append(label)
    return npm, members, other, substituted


# ─── layout ───────────────────────────────────────────────────────────────────


def primaries(by_key, declared_keys):
    """_primary_resolutions: the declared resolution of a name, else the highest."""
    by_name = {}
    for key, package in by_key.items():
        by_name.setdefault(package.name, {})[key] = package
    declared = {}
    for key in declared_keys:
        name = by_key[key].name
        if name in declared and declared[name] != key:
            sys.exit("bench_forest: the target declares two resolutions of {}: {} and {}".format(name, declared[name], key))
        declared[name] = key
    primary = {}
    for name, resolutions in by_name.items():
        if name in declared:
            primary[name] = declared[name]
            continue
        best = None
        for key in sorted(resolutions):
            if best is None or resolutions[key].rank() > resolutions[best].rank():
                best = key
        primary[name] = best
    return primary


def store_path(package, primary):
    if primary[package.name] == package.key:
        return "node_modules/" + package.name
    return "node_modules/.pnpm/{}/node_modules/{}".format(package.store_dir(), package.name)


def resolution_edges(by_key, primary, store):
    """_resolution_edges: per dependent, the deps the flat top level cannot answer."""
    edges = {}
    for key in sorted(by_key):
        for dep_label in by_key[key].dep_labels:
            dep = store.package(dep_label)
            if dep is None or primary.get(dep.name) == dep.key:
                continue
            edges.setdefault(key, []).append((dep.name, dep.key))
    return edges


def link_tree(src, dst):
    """A real directory of per-file symlinks mirroring `src`."""
    count = 0
    for root, dirs, files in os.walk(src):
        rel = os.path.relpath(root, src)
        target_dir = dst if rel == "." else os.path.join(dst, rel)
        os.makedirs(target_dir, exist_ok=True)
        for d in list(dirs):
            full = os.path.join(root, d)
            if os.path.islink(full):
                os.symlink(os.path.realpath(full), os.path.join(target_dir, d))
                dirs.remove(d)
                count += 1
        for f in files:
            os.symlink(os.path.join(root, f), os.path.join(target_dir, f))
            count += 1
    return count


def rewrite_manifest_targets(value, declaration_role):
    """Source-file targets in a manifest field, spelled as the emitted files."""
    if isinstance(value, str):
        for suffix, emitted in (SOURCE_TO_DTS if declaration_role else SOURCE_TO_JS).items():
            if value.endswith(suffix) and not is_declaration(value):
                return value[: -len(suffix)] + emitted
        return value
    if isinstance(value, dict):
        return {k: rewrite_manifest_targets(v, declaration_role or k == "types") for k, v in value.items()}
    if isinstance(value, list):
        return [rewrite_manifest_targets(v, declaration_role) for v in value]
    return value


def rewrite_manifest(manifest):
    out = dict(manifest)
    for field in ("main", "module", "browser", "exports", "imports"):
        if field in out:
            out[field] = rewrite_manifest_targets(out[field], False)
    for field in ("types", "typings"):
        if field in out:
            out[field] = rewrite_manifest_targets(out[field], True)
    return out


def stage_inputs(execroot, replica, inputs, copy_paths, skip, source_root=None):
    """Every listed input at its exec-root path: a symlink, a copy for `copy_paths`, expanded for a directory.

    `source_root(path)` names the tree a path is read from when it is not the execroot.
    """
    staged = 0
    for path in inputs:
        if skip(path):
            continue
        src = os.path.join(source_root(path) if source_root else execroot, path)
        dst = os.path.join(replica, path)
        if os.path.isdir(src) and not os.path.islink(src):
            staged += link_tree(src, dst)
            continue
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        if path in copy_paths:
            shutil.copyfile(src, dst)
        else:
            os.symlink(src, dst)
        staged += 1
    return staged


# ─── tsconfig ─────────────────────────────────────────────────────────────────


def read_jsonc(path):
    """tsconfig JSON with its comments and trailing commas removed, strings left alone."""
    text = open(path).read()
    out = []
    i = 0
    while i < len(text):
        ch = text[i]
        if ch == '"':
            end = i + 1
            while end < len(text) and text[end] != '"':
                end += 2 if text[end] == "\\" else 1
            out.append(text[i : end + 1])
            i = end + 1
        elif text.startswith("//", i):
            i = text.find("\n", i)
            i = len(text) if i < 0 else i
        elif text.startswith("/*", i):
            i = text.find("*/", i + 2)
            i = len(text) if i < 0 else i + 2
        else:
            out.append(ch)
            i += 1
    return json.loads(re.sub(r",(\s*[}\]])", r"\1", "".join(out)))


def user_types(tsconfig_abs, user_extends):
    """compilerOptions.types as the user's tsconfig chain sets it, nearest file first."""
    base_dir = os.path.dirname(tsconfig_abs)
    for entry in user_extends:
        path = os.path.normpath(os.path.join(base_dir, entry))
        while path:
            config = read_jsonc(path)
            options = config.get("compilerOptions", {})
            if "types" in options:
                return options["types"], path
            parent = config.get("extends")
            if isinstance(parent, list):
                parent = parent[0] if parent else None
            path = os.path.normpath(os.path.join(os.path.dirname(path), parent)) if parent else None
    return None, None


def package_of_key(key):
    parts = key.split("/")
    return "/".join(parts[:2]) if key.startswith("@") and len(parts) > 1 else parts[0]


def check_variant(config):
    out = json.loads(json.dumps(config))
    options = out["compilerOptions"]
    options["declaration"] = False
    options["emitDeclarationOnly"] = False
    options["noEmit"] = True
    options.pop("declarationDir", None)
    return out


def write_json(path, data):
    with open(path, "w") as f:
        json.dump(data, f, indent=2)
        f.write("\n")


# ─── subcommands ──────────────────────────────────────────────────────────────


def cmd_members(args):
    aquery = read_aquery(args.aquery)
    prefix, _ = npm_prefix_and_repos(aquery["inputs"], args.execroot)
    hub = Hub(args.execroot, prefix)
    _, members, _, _ = classify_deps(rule_deps(args.deps_xml, args.label), hub, Store(args.execroot, prefix), package_dir_of(args.label))
    for member in members:
        print("{}\t//{}".format(member["view"], canonical(member["target"]).split("//", 1)[1]))


def cmd_build(args):
    execroot, workspace = args.execroot, args.workspace
    aquery = read_aquery(args.aquery)
    inputs = aquery["inputs"]
    prefix, input_repos = npm_prefix_and_repos(inputs, execroot)
    input_repo_set = set(input_repos)
    store = Store(execroot, prefix)
    hub = Hub(execroot, prefix)

    def in_npm_repo(path):
        return path.startswith("external/") and path.split("/")[1] in input_repo_set

    importer_dir = package_dir_of(args.label)
    declared_labels, members, other_deps, substituted = classify_deps(rule_deps(args.deps_xml, args.label), hub, store, importer_dir)
    member_declared = {}
    for member in members:
        member_label = "//" + member["target"].split("//", 1)[1]
        labels, member_members, _, member_subst = classify_deps(rule_deps(args.members_xml, member_label), hub, store, package_dir_of(member_label))
        if member_members:
            sys.exit("bench_forest: {} depends on workspace members of its own ({}); nest the run one level up".format(member_label, ", ".join(m["package_name"] for m in member_members)))
        member_declared[member["view"]] = labels
        for name, pair in member_subst.items():
            substituted["{}: {}".format(member["package_name"], name)] = pair

    # The closure: what the importer-resolved deps reach, plus the `pkg` stanza of
    # every repository the action stages that they did not reach (the paths map's
    # closure under the flat labels). A repository reached through an alias stanza
    # alone stays that alias.
    roots = list(declared_labels)
    for labels in member_declared.values():
        roots += labels
    reached, _ = store.closure(roots)
    reached_repos = {p.repo for p in reached.values()}
    roots += ["{}//:pkg".format(r) for r in input_repos if r not in reached_repos]
    closure, unextracted = store.closure(roots)
    by_key = {p.key: p for p in closure.values()}
    beyond_inputs = sorted(p.key for p in closure.values() if p.repo not in input_repo_set)
    declared_keys = [store.package(l).key for l in declared_labels]
    primary = primaries(by_key, declared_keys)
    edges = resolution_edges(by_key, primary, store)
    positions = {key: store_path(p, primary) for key, p in by_key.items()}

    tsconfig_rel = aquery["tsconfig"]
    tsconfig_dir = os.path.dirname(tsconfig_rel)
    bin_root = "/".join(tsconfig_rel.split("/")[:3])

    # A member whose declarations were emitted under its own forest is read from
    # there: what the paths map emitted carries specifiers only the paths map
    # resolves (a `paths` wildcard turns a package-internal path into a valid
    # module name), so consuming those would measure the emitter, not the forest.
    member_source = {}
    for member in members:
        target_pkg = package_dir_of(member["target"])
        forest_dir = os.path.join(args.member_forest_root, member["view"], "forest") if args.member_forest_root else None
        if forest_dir and os.path.isdir(os.path.join(forest_dir, bin_root, target_pkg)):
            member_source["{}/{}/".format(bin_root, target_pkg)] = forest_dir

    def forest_source_root(path):
        for member_prefix, root in member_source.items():
            if path.startswith(member_prefix):
                return root
        return execroot

    unemitted = [p for p in inputs if forest_source_root(p) != execroot and not os.path.lexists(os.path.join(forest_source_root(p), p))]
    if unemitted:
        sys.exit("bench_forest: {} member declarations the target reads were not emitted under the member's forest, e.g. {}".format(len(unemitted), unemitted[0]))

    action_config = json.load(open(os.path.join(execroot, tsconfig_rel)))
    extends = action_config.get("extends", [])
    if isinstance(extends, str):
        extends = [extends]
    bin_extends = [
        os.path.normpath(os.path.join(tsconfig_dir, e)) for e in extends if os.path.normpath(os.path.join(tsconfig_dir, e)).startswith(bin_root + "/")
    ]
    user_extends = [e for e in extends if not os.path.normpath(os.path.join(tsconfig_dir, e)).startswith(bin_root + "/")]
    copy_paths = set([tsconfig_rel] + bin_extends)

    for replica in (args.paths, args.forest):
        if os.path.exists(replica):
            shutil.rmtree(replica)
        os.makedirs(replica)

    paths_staged = stage_inputs(execroot, args.paths, inputs, copy_paths, lambda p: False)
    forest_first_party = stage_inputs(execroot, args.forest, inputs, copy_paths, in_npm_repo, forest_source_root)

    # The forest: every resolution at its store position, linked whole when
    # nothing nests under it and mirrored file by file when its own deps do.
    forest_root = args.forest
    linked_whole = materialised = nested_links = 0
    for key, package in by_key.items():
        pos = os.path.join(forest_root, positions[key])
        src = os.path.join(execroot, "external", package.repo, package.package_root)
        if not os.path.isdir(src):
            sys.exit("bench_forest: {} is not extracted in the execroot ({})".format(key, src))
        os.makedirs(os.path.dirname(pos), exist_ok=True)
        if key in edges:
            link_tree(src, pos)
            materialised += 1
            for dep_name, dep_key in edges[key]:
                link = os.path.join(pos, "node_modules", dep_name)
                os.makedirs(os.path.dirname(link), exist_ok=True)
                os.symlink(os.path.join(forest_root, positions[dep_key]), link)
                nested_links += 1
        else:
            os.symlink(src, pos)
            linked_whole += 1

    # Workspace members: the link: name over the compiling target's bin-dir
    # files and the member's checked-in declarations, at package-relative paths,
    # under the member's manifest with its source-file targets rewritten.
    member_records = []
    member_files = member_links = 0
    for member in members:
        target_pkg = package_dir_of(member["target"])
        member_dir = member["member_dir"]
        offset = os.path.relpath(target_pkg, member_dir) if target_pkg != member_dir else ""
        link_dir = os.path.join(forest_root, "node_modules", member["package_name"])
        os.makedirs(link_dir, exist_ok=True)
        manifest = read_jsonc(os.path.join(workspace, member_dir, "package.json"))
        rewritten = rewrite_manifest(manifest)
        write_json(os.path.join(link_dir, "package.json"), rewritten)
        bin_prefix = "{}/{}/".format(bin_root, target_pkg)
        src_prefix = member_dir + "/"
        files = 0
        for path in inputs:
            if path.startswith(bin_prefix):
                rel = os.path.join(offset, path[len(bin_prefix):]) if offset else path[len(bin_prefix):]
            elif path.startswith(src_prefix) and not path.startswith("external/"):
                rel = path[len(src_prefix):]
            else:
                continue
            dst = os.path.join(link_dir, rel)
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            os.symlink(os.path.join(forest_source_root(path), path), dst)
            files += 1
        links = []
        for label in member_declared[member["view"]]:
            package = store.package(label)
            if primary[package.name] == package.key:
                continue
            link = os.path.join(link_dir, "node_modules", package.name)
            os.makedirs(os.path.dirname(link), exist_ok=True)
            os.symlink(os.path.join(forest_root, positions[package.key]), link)
            links.append("{} -> {}".format(package.name, positions[package.key]))
        member_files += files
        member_links += len(links)
        exports = rewritten.get("exports")
        member_records.append({
            "package_name": member["package_name"],
            "view": member["view"],
            "member_dir": member_dir,
            "target": "//" + member["target"].split("//", 1)[1],
            "declarations_from": member_source.get(bin_prefix, execroot),
            "files": files,
            "nested_links": links,
            "exports_root": exports.get(".") if isinstance(exports, dict) else rewritten.get("main"),
        })

    # The forest tsconfig: the action's, minus every npm and member `paths` key,
    # with `types` as the user's chain sets it and `typeRoots` on the forest.
    member_names = {m["package_name"] for m in members}
    npm_names = {p.name for p in by_key.values()}
    kept_paths = {}
    dropped = {"npm": 0, "member": 0}
    for key, value in action_config["compilerOptions"].get("paths", {}).items():
        package = package_of_key(key)
        if package in member_names:
            dropped["member"] += 1
        elif package in npm_names or package.startswith("@types/") or any("/external/" in v or v.startswith("external/") for v in value):
            dropped["npm"] += 1
        else:
            kept_paths[key] = value
    types, types_source = user_types(os.path.join(execroot, tsconfig_rel), user_extends)
    if types is None:
        types = sorted(by_key[k].name[len("@types/"):] for k in declared_keys if by_key[k].name.startswith("@types/"))
        types_source = "the direct @types deps (the user's chain sets none)"
    forest_config = json.loads(json.dumps(action_config))
    forest_config["compilerOptions"]["paths"] = kept_paths
    forest_config["compilerOptions"]["types"] = types
    forest_config["compilerOptions"]["typeRoots"] = [os.path.relpath(os.path.join(forest_root, "node_modules", "@types"), os.path.join(forest_root, tsconfig_dir))]
    forest_config["files"] = []

    stem = tsconfig_rel[: -len(".tsconfig.json")]
    configs = {
        "paths": {"emit": tsconfig_rel, "check": stem + ".check.tsconfig.json"},
        "forest": {"emit": stem + ".forest.tsconfig.json", "check": stem + ".forest.check.tsconfig.json"},
    }
    write_json(os.path.join(args.paths, configs["paths"]["check"]), check_variant(action_config))
    write_json(os.path.join(args.forest, configs["forest"]["emit"]), forest_config)
    write_json(os.path.join(args.forest, configs["forest"]["check"]), check_variant(forest_config))

    # Nothing the emit writes may land on a staged link: a symlink there would
    # write through into the real bin dir.
    options = action_config["compilerOptions"]
    root_dir = os.path.normpath(os.path.join(tsconfig_dir, options["rootDir"]))
    out_dir = os.path.normpath(os.path.join(tsconfig_dir, options.get("declarationDir", options.get("outDir", "."))))
    expected_outputs = []
    for entry in action_config.get("include", []):
        src = os.path.normpath(os.path.join(tsconfig_dir, entry))
        if is_declaration(src) or not src.endswith((".ts", ".tsx", ".mts", ".cts")):
            continue
        rel = os.path.relpath(src, root_dir)
        for suffix, emitted in SOURCE_TO_DTS.items():
            if rel.endswith(suffix):
                expected_outputs.append(os.path.join(out_dir, rel[: -len(suffix)] + emitted))
                break
    for replica in (args.paths, args.forest):
        collisions = [o for o in expected_outputs if os.path.lexists(os.path.join(replica, o))]
        if collisions:
            sys.exit("bench_forest: {} emit outputs already exist in {}, e.g. {}".format(len(collisions), replica, collisions[0]))

    names = {}
    for p in by_key.values():
        names.setdefault(p.name, []).append(p.key)
    multi = sorted(n for n, keys in names.items() if len(keys) > 1)
    plan = {
        "label": args.label,
        "execroot": execroot,
        "workspace": workspace,
        "tsc": os.path.join(execroot, aquery["tsc"]),
        "tsconfig_dir": tsconfig_dir,
        "bin_root": bin_root,
        "out_dir": out_dir,
        "expected_outputs": len(expected_outputs),
        "action_outputs": len(aquery["outputs"]),
        "replicas": {"paths": args.paths, "forest": args.forest},
        "configs": configs,
        "counts": {
            "inputs": len(inputs),
            "paths_replica_staged": paths_staged,
            "forest_first_party_staged": forest_first_party,
            "npm_repos_among_inputs": len(input_repos),
            "packages_in_closure": len(by_key),
            "packages_beyond_the_inputs": len(beyond_inputs),
            "edges_to_unextracted_repositories": len(unextracted),
            "types_packages_in_closure": sum(1 for p in by_key.values() if p.name.startswith("@types/")),
            "declared_npm_deps": len(declared_keys),
            "declared_deps_resolved_by_the_importer_not_the_flat_hub": len([k for k in substituted if ": " not in k]),
            "member_views": len(members),
            "other_deps": len(other_deps),
            "names_with_several_resolutions": len(multi),
            "top_level_names": len(primary),
            "linked_whole": linked_whole,
            "materialised": materialised,
            "nested_links": nested_links,
            "member_files": member_files,
            "member_nested_links": member_links,
            "paths_keys_action": len(action_config["compilerOptions"].get("paths", {})),
            "paths_keys_forest": len(kept_paths),
            "paths_keys_dropped": dropped,
        },
        "packages_beyond_the_inputs": beyond_inputs,
        "edges_to_unextracted_repositories": unextracted,
        "declared_substitutions": substituted,
        "other_deps": other_deps,
        "names_with_several_resolutions": multi,
        "primary_of_multi": {name: primary[name] for name in multi},
        "forest_paths_kept": sorted(kept_paths),
        "forest_types": types,
        "forest_types_source": types_source,
        "action_types": options.get("types"),
        "action_files": action_config.get("files", []),
        "members": member_records,
    }
    write_json(args.plan, plan)
    json.dump(plan["counts"], sys.stdout, indent=2)
    print()


# ─── compare ──────────────────────────────────────────────────────────────────


def program_files(explain_out):
    """(file, [reason lines]) for every file an --explainFiles run lists."""
    files = []
    for line in open(explain_out, errors="replace"):
        line = line.rstrip("\n")
        if not line:
            continue
        if line.startswith(" "):
            if files:
                files[-1][1].append(line.strip())
            continue
        if ": error TS" in line or not line.endswith(PROGRAM_SUFFIXES):
            continue
        files.append((line, []))
    return files


def normalise(path, members_by_bin, members_by_src):
    """One name for a file whichever tree it was reached through."""
    if "node_modules/" in path:
        return path[path.rindex("node_modules/") + len("node_modules/"):]
    for prefix, name in members_by_bin.items():
        if path.startswith(prefix):
            return name + "/" + path[len(prefix):]
    for prefix, name in members_by_src.items():
        if path.startswith(prefix):
            return name + "/" + path[len(prefix):]
    return path


def diagnostics(check_out, members_by_bin, members_by_src):
    out = []
    for line in open(check_out, errors="replace"):
        if ": error TS" not in line:
            continue
        location, _, rest = line.partition(": error TS")
        path = location.split("(")[0]
        out.append(normalise(path, members_by_bin, members_by_src) + location[len(path):] + ": error TS" + rest.rstrip("\n"))
    return sorted(out)


def package_name_of(normalised):
    parts = normalised.split("/")
    return "/".join(parts[:2]) if normalised.startswith("@") and len(parts) > 1 else parts[0]


def cmd_compare(args):
    plan = json.load(open(args.plan))
    members_by_bin = {"{}/{}/".format(plan["bin_root"], package_dir_of(m["target"])): m["package_name"] for m in plan["members"]}
    members_by_src = {m["member_dir"] + "/": m["package_name"] for m in plan["members"]}

    forest_diag = diagnostics(args.forest_check, members_by_bin, members_by_src)
    paths_diag = diagnostics(args.paths_check, members_by_bin, members_by_src)
    print("diagnostics: forest {} error TS lines, paths {} error TS lines".format(len(forest_diag), len(paths_diag)))
    only_forest = sorted(set(forest_diag) - set(paths_diag))
    only_paths = sorted(set(paths_diag) - set(forest_diag))
    for label, lines in (("only in the forest", only_forest), ("only under the paths map", only_paths)):
        print("  {}: {}".format(label, len(lines)))
        for line in lines[:40]:
            print("    " + line)
        if len(lines) > 40:
            print("    [... {} lines ...]".format(len(lines) - 40))

    if not (args.forest_explain and args.paths_explain):
        if only_forest or only_paths:
            print("DIAGNOSTICS DIFFER: the walls do not count")
            sys.exit(1)
        print("DIAGNOSTICS EQUAL")
        return

    # tsgo prints its default libraries at the binary's real location, past the execroot's symlink.
    lib_dir = os.path.realpath(os.path.dirname(plan["tsc"])) + "/"
    forest = program_files(args.forest_explain)
    paths = program_files(args.paths_explain)
    forest_norm = {normalise(f, members_by_bin, members_by_src): (f, r) for f, r in forest}
    paths_norm = {normalise(f, members_by_bin, members_by_src): (f, r) for f, r in paths}
    print("program: forest {} files, paths {} files, {} in both".format(len(forest), len(paths), len(set(forest_norm) & set(paths_norm))))
    for label, only in (("only in the forest", set(forest_norm) - set(paths_norm)), ("only under the paths map", set(paths_norm) - set(forest_norm))):
        by_package = {}
        for n in only:
            by_package.setdefault(package_name_of(n), []).append(n)
        print("  {}: {} files in {} packages".format(label, len(only), len(by_package)))
        for package, files in sorted(by_package.items(), key=lambda kv: -len(kv[1]))[:30]:
            print("    {:5d} {}   e.g. {}".format(len(files), package, sorted(files)[0]))
        if len(by_package) > 30:
            print("    [... {} packages ...]".format(len(by_package) - 30))

    # Invariant 1: every npm file the forest program holds is under
    # node_modules/<name>, none escaped to a realpath outside the replica.
    forest_paths = [f for f, _ in forest]
    under_nm = [f for f in forest_paths if "node_modules/" in f]
    toolchain_libs = [f for f in forest_paths if os.path.realpath(os.path.join(plan["replicas"]["forest"], f)).startswith(lib_dir)]
    escaped = [f for f in forest_paths if (f.startswith(("/", "../")) or "/external/" in f or f.startswith("external/")) and f not in toolchain_libs]
    ts_entries = [f for f in under_nm if f.endswith((".ts", ".tsx", ".mts", ".cts")) and not is_declaration(f)]
    print("forest program: {} files under node_modules/, {} toolchain lib files, {} outside the replica (realpath escapes)".format(len(under_nm), len(toolchain_libs), len(escaped)))
    for f in escaped[:10]:
        print("    escaped: " + f)
    print("  non-declaration TypeScript under node_modules/ (library files, never emitted): {}".format(len(ts_entries)))
    for f in ts_entries:
        print("    " + f)
        for reason in forest_norm[normalise(f, members_by_bin, members_by_src)][1][:2]:
            print("      " + reason)

    for package in args.package or []:
        for arm, table in (("forest", forest_norm), ("paths", paths_norm)):
            hits = sorted(n for n in table if package_name_of(n) == package)
            print("{}: {} -- {} files".format(arm, package, len(hits)))
            for n in hits[:6]:
                path, reasons = table[n]
                print("    " + path)
                for reason in reasons[:2]:
                    print("      " + reason)
            if len(hits) > 6:
                print("    [... {} files ...]".format(len(hits) - 6))

    if only_forest or only_paths:
        print("DIAGNOSTICS DIFFER: the walls do not count")
        sys.exit(1)
    print("DIAGNOSTICS EQUAL")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    members = sub.add_parser("members")
    for flag in ("--aquery", "--deps-xml", "--execroot", "--label"):
        members.add_argument(flag, required=True)
    members.set_defaults(func=cmd_members)
    build = sub.add_parser("build")
    for flag in ("--aquery", "--deps-xml", "--members-xml", "--workspace", "--execroot", "--label", "--paths", "--forest", "--plan"):
        build.add_argument(flag, required=True)
    build.add_argument("--member-forest-root", help="directory holding <view>/forest for members already emitted under a forest")
    build.set_defaults(func=cmd_build)
    compare = sub.add_parser("compare")
    for flag in ("--plan", "--forest-check", "--paths-check"):
        compare.add_argument(flag, required=True)
    compare.add_argument("--forest-explain")
    compare.add_argument("--paths-explain")
    compare.add_argument("--package", action="append")
    compare.set_defaults(func=cmd_compare)
    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
