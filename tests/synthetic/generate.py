#!/usr/bin/env python3
"""Generator for the synthetic diamond-dependency test graph.

Creates N packages arranged in layers:
  - Layer 0 (base):   pkg_00  — no deps
  - Layer 1 (mid_a):  pkg_01 .. pkg_04  — each depends on pkg_00
  - Layer 2 (mid_b):  pkg_05 .. pkg_09  — each depends on one of the layer-1 pkgs
  - Layer 3 (leaf):   pkg_10 .. pkg_19  — each depends on two layer-2 pkgs
                                          (creating a true diamond)

For N=20 that gives:
  1 base + 4 mid_a + 5 mid_b + 10 leaf = 20 packages

Usage:
  python3 tests/synthetic/generate.py [--out-dir tests/synthetic] [--n 20]
"""

import argparse
import os
import sys
import textwrap

# ─── constants ────────────────────────────────────────────────────────────────

TOTAL = 20          # total packages
N_BASE = 1          # pkg_00
N_MID_A = 4         # pkg_01 .. pkg_04
N_MID_B = 5         # pkg_05 .. pkg_09
N_LEAF = 10         # pkg_10 .. pkg_19

BASE_RANGE  = range(0, N_BASE)
MID_A_RANGE = range(N_BASE, N_BASE + N_MID_A)
MID_B_RANGE = range(N_BASE + N_MID_A, N_BASE + N_MID_A + N_MID_B)
LEAF_RANGE  = range(N_BASE + N_MID_A + N_MID_B, TOTAL)

def pkg_name(i: int) -> str:
    return "pkg_{:02d}".format(i)

def dep_list(indices):
    """Return the list of dep indices for a given package."""
    return indices


def make_deps(i: int):
    """Return direct dep indices for package i."""
    if i in BASE_RANGE:
        return []
    if i in MID_A_RANGE:
        # All mid_a packages depend on pkg_00 (the single base)
        return [0]
    if i in MID_B_RANGE:
        # mid_b[j] depends on mid_a[j % N_MID_A]
        j = i - N_BASE - N_MID_A        # 0-based index within mid_b
        return [N_BASE + (j % N_MID_A)]
    if i in LEAF_RANGE:
        # leaf[j] depends on two mid_b packages to create a diamond
        j = i - N_BASE - N_MID_A - N_MID_B   # 0-based index within leaf
        dep_a = N_BASE + N_MID_A + (j % N_MID_B)
        dep_b = N_BASE + N_MID_A + ((j + 1) % N_MID_B)
        # Deduplicate in case both indices happen to be the same
        deps = [dep_a]
        if dep_b != dep_a:
            deps.append(dep_b)
        return deps
    return []


# ─── TypeScript file templates ────────────────────────────────────────────────

def ts_file_types(i: int, dep_indices) -> str:
    """Generate the types.ts file for pkg_i."""
    pn = pkg_name(i)
    idx = i  # used in type names

    import_lines = []
    for d in dep_indices:
        dn = pkg_name(d)
        import_lines.append('import type {{ Value{d:02d} }} from "../{dn}/types";'.format(
            d=d, dn=dn))

    base_field = ""
    for d in dep_indices:
        base_field += "\n  base{d:02d}: Value{d:02d};".format(d=d)

    imports = "\n".join(import_lines)
    if imports:
        imports += "\n\n"

    return textwrap.dedent("""\
        {imports}export interface Value{idx:02d} {{
          id: string;
          value: number;{base_field}
        }}

        export type Tag{idx:02d} = string;
        """).format(imports=imports, idx=idx, base_field=base_field)


def ts_file_utils(i: int, dep_indices) -> str:
    """Generate the utils.ts file for pkg_i."""
    pn = pkg_name(i)
    idx = i

    import_lines = []
    for d in dep_indices:
        dn = pkg_name(d)
        import_lines.append('import type {{ Value{d:02d} }} from "../{dn}/types";'.format(
            d=d, dn=dn))
    import_lines.append('import type {{ Value{idx:02d}, Tag{idx:02d} }} from "./types";'.format(idx=idx))

    imports = "\n".join(import_lines)

    # Build the compute function: consumes deps' values, returns own Value
    dep_params = ""
    dep_body_lines = []
    for d in dep_indices:
        dep_params += ", dep{d:02d}: Value{d:02d}".format(d=d)
        dep_body_lines.append("    base{d:02d}: dep{d:02d},".format(d=d))

    dep_body = "\n".join(dep_body_lines)
    if dep_body:
        dep_body = "\n" + dep_body

    return textwrap.dedent("""\
        {imports}

        export function compute{idx:02d}(id: string, raw: number{dep_params}): Value{idx:02d} {{
          return {{
            id,
            value: raw * {idx},{dep_body}
          }};
        }}

        export function label{idx:02d}(v: Value{idx:02d}): Tag{idx:02d} {{
          return `pkg{idx:02d}:${{v.id}}:${{v.value}}`;
        }}
        """).format(imports=imports, idx=idx, dep_params=dep_params, dep_body=dep_body)


def ts_file_index(i: int, dep_indices) -> str:
    """Generate the index.ts file for pkg_i (re-exports public API)."""
    idx = i
    return textwrap.dedent("""\
        export type {{ Value{idx:02d}, Tag{idx:02d} }} from "./types";
        export {{ compute{idx:02d}, label{idx:02d} }} from "./utils";
        """).format(idx=idx)


def build_bazel(i: int, dep_indices, out_dir: str) -> str:
    """Generate the BUILD.bazel for pkg_i."""
    pn = pkg_name(i)
    dep_strs = []
    for d in dep_indices:
        dn = pkg_name(d)
        dep_strs.append('        "//tests/synthetic/{dn}",'.format(dn=dn))
    deps_attr = ""
    if dep_strs:
        deps_attr = "\n    deps = [\n{}\n    ],".format("\n".join(dep_strs))

    return textwrap.dedent("""\
        load("//ts:defs.bzl", "ts_compile")

        ts_compile(
            name = "{pn}",
            srcs = [
                "index.ts",
                "types.ts",
                "utils.ts",
            ],
            visibility = ["//tests/synthetic:__subpackages__"],{deps_attr}
        )
        """).format(pn=pn, deps_attr=deps_attr)


# ─── top-level BUILD.bazel ─────────────────────────────────────────────────────

def top_build_bazel() -> str:
    """Generate the top-level tests/synthetic/BUILD.bazel."""
    return textwrap.dedent("""\
        # Synthetic diamond-dependency graph for incremental build verification.
        # Generated by tests/synthetic/generate.py — DO NOT EDIT BY HAND.
        #
        # Package layout:
        #   Layer 0 (base):   pkg_00
        #   Layer 1 (mid_a):  pkg_01 .. pkg_04
        #   Layer 2 (mid_b):  pkg_05 .. pkg_09
        #   Layer 3 (leaf):   pkg_10 .. pkg_19
        package(default_visibility = ["//visibility:public"])
        """)


# ─── main ─────────────────────────────────────────────────────────────────────

def generate(out_dir: str) -> None:
    # Write top-level BUILD.bazel
    top_build = os.path.join(out_dir, "BUILD.bazel")
    write(top_build, top_build_bazel())

    for i in range(TOTAL):
        deps = make_deps(i)
        pn = pkg_name(i)
        pkg_dir = os.path.join(out_dir, pn)
        os.makedirs(pkg_dir, exist_ok=True)

        write(os.path.join(pkg_dir, "types.ts"),  ts_file_types(i, deps))
        write(os.path.join(pkg_dir, "utils.ts"),  ts_file_utils(i, deps))
        write(os.path.join(pkg_dir, "index.ts"),  ts_file_index(i, deps))
        write(os.path.join(pkg_dir, "BUILD.bazel"), build_bazel(i, deps, out_dir))

    print("Generated {} packages in {}".format(TOTAL, out_dir))
    print("Layers:")
    print("  Base  (pkg_00):            {}".format(list(BASE_RANGE)))
    print("  Mid-A (pkg_01..pkg_04):    {}".format(list(MID_A_RANGE)))
    print("  Mid-B (pkg_05..pkg_09):    {}".format(list(MID_B_RANGE)))
    print("  Leaf  (pkg_10..pkg_19):    {}".format(list(LEAF_RANGE)))


def write(path: str, content: str) -> None:
    with open(path, "w") as f:
        f.write(content)
    print("  wrote", path)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--out-dir",
        default=os.path.join(os.path.dirname(__file__)),
        help="Output directory (default: same dir as this script)",
    )
    args = parser.parse_args()
    generate(os.path.abspath(args.out_dir))
