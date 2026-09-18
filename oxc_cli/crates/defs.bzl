load("@bazel_skylib//lib:selects.bzl", "selects")
load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
load("@bazel_tools//tools/build_defs/repo:utils.bzl", "maybe")

_COMMON_CONDITION = ""

def _flatten_dependency_maps(all_dependency_maps):
    dependencies = {}

    for workspace_deps_map in all_dependency_maps:
        for pkg_name, conditional_deps_map in workspace_deps_map.items():
            if pkg_name not in dependencies:
                non_frozen_map = dict()
                for key, values in conditional_deps_map.items():
                    non_frozen_map.update({key: dict(values.items())})
                dependencies.setdefault(pkg_name, non_frozen_map)
                continue

            for condition, deps_map in conditional_deps_map.items():
                if condition not in dependencies[pkg_name]:
                    dependencies[pkg_name].setdefault(condition, dict(deps_map.items()))
                    continue

                inconsistent_entries = []
                for crate_name, crate_label in deps_map.items():
                    existing = dependencies[pkg_name][condition].get(crate_name)
                    if existing and existing != crate_label:
                        inconsistent_entries.append((crate_name, existing, crate_label))
                    dependencies[pkg_name][condition].update({crate_name: crate_label})

    return dependencies

def crate_deps(deps, package_name = None):
    if not deps:
        return []

    if package_name == None:
        package_name = native.package_name()

    dependencies = _flatten_dependency_maps([
        _NORMAL_DEPENDENCIES,
        _NORMAL_DEV_DEPENDENCIES,
        _PROC_MACRO_DEPENDENCIES,
        _PROC_MACRO_DEV_DEPENDENCIES,
        _BUILD_DEPENDENCIES,
        _BUILD_PROC_MACRO_DEPENDENCIES,
    ]).pop(package_name, {})

    flat_deps = {}
    for deps_set in dependencies.values():
        for crate_name, crate_label in deps_set.items():
            flat_deps.update({crate_name: crate_label})

    missing_crates = []
    crate_targets = []
    for crate_target in deps:
        if crate_target not in flat_deps:
            missing_crates.append(crate_target)
        else:
            crate_targets.append(flat_deps[crate_target])

    if missing_crates:
        fail("Could not find crates `{}` among dependencies of `{}`. Available dependencies were `{}`".format(
            missing_crates,
            package_name,
            dependencies,
        ))

    return crate_targets

def all_crate_deps(
        normal = False,
        normal_dev = False,
        proc_macro = False,
        proc_macro_dev = False,
        build = False,
        build_proc_macro = False,
        package_name = None):
    if package_name == None:
        package_name = native.package_name()

    all_dependency_maps = []
    if normal:
        all_dependency_maps.append(_NORMAL_DEPENDENCIES)
    if normal_dev:
        all_dependency_maps.append(_NORMAL_DEV_DEPENDENCIES)
    if proc_macro:
        all_dependency_maps.append(_PROC_MACRO_DEPENDENCIES)
    if proc_macro_dev:
        all_dependency_maps.append(_PROC_MACRO_DEV_DEPENDENCIES)
    if build:
        all_dependency_maps.append(_BUILD_DEPENDENCIES)
    if build_proc_macro:
        all_dependency_maps.append(_BUILD_PROC_MACRO_DEPENDENCIES)

    if not all_dependency_maps:
        all_dependency_maps.append(_NORMAL_DEPENDENCIES)

    dependencies = _flatten_dependency_maps(all_dependency_maps).pop(package_name, None)

    if not dependencies:
        if dependencies == None:
            fail("Tried to get all_crate_deps for package " + package_name + " but that package had no Cargo.toml file")
        else:
            return []

    crate_deps = list(dependencies.pop(_COMMON_CONDITION, {}).values())
    for condition, deps in dependencies.items():
        crate_deps += selects.with_or({
            tuple(_CONDITIONS[condition]): deps.values(),
            "//conditions:default": [],
        })

    return crate_deps

def aliases(
        normal = False,
        normal_dev = False,
        proc_macro = False,
        proc_macro_dev = False,
        build = False,
        build_proc_macro = False,
        package_name = None):
    if package_name == None:
        package_name = native.package_name()

    all_aliases_maps = []
    if normal:
        all_aliases_maps.append(_NORMAL_ALIASES)
    if normal_dev:
        all_aliases_maps.append(_NORMAL_DEV_ALIASES)
    if proc_macro:
        all_aliases_maps.append(_PROC_MACRO_ALIASES)
    if proc_macro_dev:
        all_aliases_maps.append(_PROC_MACRO_DEV_ALIASES)
    if build:
        all_aliases_maps.append(_BUILD_ALIASES)
    if build_proc_macro:
        all_aliases_maps.append(_BUILD_PROC_MACRO_ALIASES)

    if not all_aliases_maps:
        all_aliases_maps.append(_NORMAL_ALIASES)
        all_aliases_maps.append(_PROC_MACRO_ALIASES)

    aliases = _flatten_dependency_maps(all_aliases_maps).pop(package_name, None)

    if not aliases:
        return dict()

    common_items = aliases.pop(_COMMON_CONDITION, {}).items()

    if not len(aliases.keys()) == 1:
        return dict(common_items)

    crate_aliases = {"//conditions:default": dict(common_items)}
    for condition, deps in aliases.items():
        condition_triples = _CONDITIONS[condition]
        for triple in condition_triples:
            if triple in crate_aliases:
                crate_aliases[triple].update(deps)
            else:
                crate_aliases.update({triple: dict(deps.items() + common_items)})

    return select(crate_aliases)

_NORMAL_DEPENDENCIES = {
    "oxc_cli": {
        _COMMON_CONDITION: {
            "clap": Label("@rules_typescript_crates//:clap-4.5.60"),
            "miette": Label("@rules_typescript_crates//:miette-7.6.0"),
            "oxc_allocator": Label("@rules_typescript_crates//:oxc_allocator-0.117.0"),
            "oxc_codegen": Label("@rules_typescript_crates//:oxc_codegen-0.117.0"),
            "oxc_diagnostics": Label("@rules_typescript_crates//:oxc_diagnostics-0.117.0"),
            "oxc_isolated_declarations": Label("@rules_typescript_crates//:oxc_isolated_declarations-0.117.0"),
            "oxc_parser": Label("@rules_typescript_crates//:oxc_parser-0.117.0"),
            "oxc_semantic": Label("@rules_typescript_crates//:oxc_semantic-0.117.0"),
            "oxc_span": Label("@rules_typescript_crates//:oxc_span-0.117.0"),
            "oxc_transformer": Label("@rules_typescript_crates//:oxc_transformer-0.117.0"),
            "rayon": Label("@rules_typescript_crates//:rayon-1.11.0"),
        },
    },
}

_NORMAL_ALIASES = {
    "oxc_cli": {
        _COMMON_CONDITION: {
        },
    },
}

_NORMAL_DEV_DEPENDENCIES = {
    "oxc_cli": {
    },
}

_NORMAL_DEV_ALIASES = {
    "oxc_cli": {
    },
}

_PROC_MACRO_DEPENDENCIES = {
    "oxc_cli": {
    },
}

_PROC_MACRO_ALIASES = {
    "oxc_cli": {
    },
}

_PROC_MACRO_DEV_DEPENDENCIES = {
    "oxc_cli": {
    },
}

_PROC_MACRO_DEV_ALIASES = {
    "oxc_cli": {
    },
}

_BUILD_DEPENDENCIES = {
    "oxc_cli": {
    },
}

_BUILD_ALIASES = {
    "oxc_cli": {
    },
}

_BUILD_PROC_MACRO_DEPENDENCIES = {
    "oxc_cli": {
    },
}

_BUILD_PROC_MACRO_ALIASES = {
    "oxc_cli": {
    },
}

_CONDITIONS = {
    "aarch64-apple-darwin": ["@rules_rust//rust/platform:aarch64-apple-darwin"],
    "aarch64-apple-ios": ["@rules_rust//rust/platform:aarch64-apple-ios"],
    "aarch64-apple-ios-macabi": ["@rules_rust//rust/platform:aarch64-apple-ios-macabi"],
    "aarch64-apple-ios-sim": ["@rules_rust//rust/platform:aarch64-apple-ios-sim"],
    "aarch64-linux-android": ["@rules_rust//rust/platform:aarch64-linux-android"],
    "aarch64-pc-windows-gnullvm": [],
    "aarch64-pc-windows-msvc": ["@rules_rust//rust/platform:aarch64-pc-windows-msvc"],
    "aarch64-unknown-fuchsia": ["@rules_rust//rust/platform:aarch64-unknown-fuchsia"],
    "aarch64-unknown-linux-gnu": ["@rules_rust//rust/platform:aarch64-unknown-linux-gnu"],
    "aarch64-unknown-nixos-gnu": ["@rules_rust//rust/platform:aarch64-unknown-nixos-gnu"],
    "aarch64-unknown-nto-qnx710": ["@rules_rust//rust/platform:aarch64-unknown-nto-qnx710"],
    "aarch64-unknown-uefi": ["@rules_rust//rust/platform:aarch64-unknown-uefi"],
    "arm-unknown-linux-gnueabi": ["@rules_rust//rust/platform:arm-unknown-linux-gnueabi"],
    "arm-unknown-linux-musleabi": ["@rules_rust//rust/platform:arm-unknown-linux-musleabi"],
    "armv7-linux-androideabi": ["@rules_rust//rust/platform:armv7-linux-androideabi"],
    "armv7-unknown-linux-gnueabi": ["@rules_rust//rust/platform:armv7-unknown-linux-gnueabi"],
    "cfg(all(any(target_arch = \"x86_64\", target_arch = \"arm64ec\"), target_env = \"msvc\", not(windows_raw_dylib)))": ["@rules_rust//rust/platform:x86_64-pc-windows-msvc"],
    "cfg(all(any(target_os = \"linux\", target_os = \"android\"), any(rustix_use_libc, miri, not(all(target_os = \"linux\", any(target_endian = \"little\", any(target_arch = \"s390x\", target_arch = \"powerpc\")), any(target_arch = \"arm\", all(target_arch = \"aarch64\", target_pointer_width = \"64\"), target_arch = \"riscv64\", all(rustix_use_experimental_asm, target_arch = \"powerpc\"), all(rustix_use_experimental_asm, target_arch = \"powerpc64\"), all(rustix_use_experimental_asm, target_arch = \"s390x\"), all(rustix_use_experimental_asm, target_arch = \"mips\"), all(rustix_use_experimental_asm, target_arch = \"mips32r6\"), all(rustix_use_experimental_asm, target_arch = \"mips64\"), all(rustix_use_experimental_asm, target_arch = \"mips64r6\"), target_arch = \"x86\", all(target_arch = \"x86_64\", target_pointer_width = \"64\")))))))": ["@rules_rust//rust/platform:aarch64-linux-android", "@rules_rust//rust/platform:armv7-linux-androideabi", "@rules_rust//rust/platform:i686-linux-android", "@rules_rust//rust/platform:powerpc-unknown-linux-gnu", "@rules_rust//rust/platform:s390x-unknown-linux-gnu", "@rules_rust//rust/platform:x86_64-linux-android"],
    "cfg(all(not(rustix_use_libc), not(miri), target_os = \"linux\", any(target_endian = \"little\", any(target_arch = \"s390x\", target_arch = \"powerpc\")), any(target_arch = \"arm\", all(target_arch = \"aarch64\", target_pointer_width = \"64\"), target_arch = \"riscv64\", all(rustix_use_experimental_asm, target_arch = \"powerpc\"), all(rustix_use_experimental_asm, target_arch = \"powerpc64\"), all(rustix_use_experimental_asm, target_arch = \"s390x\"), all(rustix_use_experimental_asm, target_arch = \"mips\"), all(rustix_use_experimental_asm, target_arch = \"mips32r6\"), all(rustix_use_experimental_asm, target_arch = \"mips64\"), all(rustix_use_experimental_asm, target_arch = \"mips64r6\"), target_arch = \"x86\", all(target_arch = \"x86_64\", target_pointer_width = \"64\"))))": ["@rules_rust//rust/platform:aarch64-unknown-linux-gnu", "@rules_rust//rust/platform:aarch64-unknown-nixos-gnu", "@rules_rust//rust/platform:arm-unknown-linux-gnueabi", "@rules_rust//rust/platform:arm-unknown-linux-musleabi", "@rules_rust//rust/platform:armv7-unknown-linux-gnueabi", "@rules_rust//rust/platform:i686-unknown-linux-gnu", "@rules_rust//rust/platform:riscv64gc-unknown-linux-gnu", "@rules_rust//rust/platform:x86_64-unknown-linux-gnu", "@rules_rust//rust/platform:x86_64-unknown-nixos-gnu"],
    "cfg(all(not(windows), any(rustix_use_libc, miri, not(all(target_os = \"linux\", any(target_endian = \"little\", any(target_arch = \"s390x\", target_arch = \"powerpc\")), any(target_arch = \"arm\", all(target_arch = \"aarch64\", target_pointer_width = \"64\"), target_arch = \"riscv64\", all(rustix_use_experimental_asm, target_arch = \"powerpc\"), all(rustix_use_experimental_asm, target_arch = \"powerpc64\"), all(rustix_use_experimental_asm, target_arch = \"s390x\"), all(rustix_use_experimental_asm, target_arch = \"mips\"), all(rustix_use_experimental_asm, target_arch = \"mips32r6\"), all(rustix_use_experimental_asm, target_arch = \"mips64\"), all(rustix_use_experimental_asm, target_arch = \"mips64r6\"), target_arch = \"x86\", all(target_arch = \"x86_64\", target_pointer_width = \"64\")))))))": ["@rules_rust//rust/platform:aarch64-apple-darwin", "@rules_rust//rust/platform:aarch64-apple-ios", "@rules_rust//rust/platform:aarch64-apple-ios-macabi", "@rules_rust//rust/platform:aarch64-apple-ios-sim", "@rules_rust//rust/platform:aarch64-linux-android", "@rules_rust//rust/platform:aarch64-unknown-fuchsia", "@rules_rust//rust/platform:aarch64-unknown-nto-qnx710", "@rules_rust//rust/platform:aarch64-unknown-uefi", "@rules_rust//rust/platform:armv7-linux-androideabi", "@rules_rust//rust/platform:i686-apple-darwin", "@rules_rust//rust/platform:i686-linux-android", "@rules_rust//rust/platform:i686-unknown-freebsd", "@rules_rust//rust/platform:powerpc-unknown-linux-gnu", "@rules_rust//rust/platform:riscv32imc-unknown-none-elf", "@rules_rust//rust/platform:riscv64gc-unknown-none-elf", "@rules_rust//rust/platform:s390x-unknown-linux-gnu", "@rules_rust//rust/platform:thumbv6m-none-eabi", "@rules_rust//rust/platform:thumbv7em-none-eabi", "@rules_rust//rust/platform:thumbv7em-none-eabihf", "@rules_rust//rust/platform:thumbv8m.main-none-eabi", "@rules_rust//rust/platform:wasm32-unknown-emscripten", "@rules_rust//rust/platform:wasm32-unknown-unknown", "@rules_rust//rust/platform:wasm32-wasip1", "@rules_rust//rust/platform:wasm32-wasip1-threads", "@rules_rust//rust/platform:wasm32-wasip2", "@rules_rust//rust/platform:x86_64-apple-darwin", "@rules_rust//rust/platform:x86_64-apple-ios", "@rules_rust//rust/platform:x86_64-apple-ios-macabi", "@rules_rust//rust/platform:x86_64-linux-android", "@rules_rust//rust/platform:x86_64-unknown-freebsd", "@rules_rust//rust/platform:x86_64-unknown-fuchsia", "@rules_rust//rust/platform:x86_64-unknown-none", "@rules_rust//rust/platform:x86_64-unknown-uefi"],
    "cfg(all(target_arch = \"aarch64\", target_env = \"msvc\", not(windows_raw_dylib)))": ["@rules_rust//rust/platform:aarch64-pc-windows-msvc"],
    "cfg(all(target_arch = \"aarch64\", target_os = \"linux\"))": ["@rules_rust//rust/platform:aarch64-unknown-linux-gnu", "@rules_rust//rust/platform:aarch64-unknown-nixos-gnu"],
    "cfg(all(target_arch = \"aarch64\", target_vendor = \"apple\"))": ["@rules_rust//rust/platform:aarch64-apple-darwin", "@rules_rust//rust/platform:aarch64-apple-ios", "@rules_rust//rust/platform:aarch64-apple-ios-macabi", "@rules_rust//rust/platform:aarch64-apple-ios-sim"],
    "cfg(all(target_arch = \"loongarch64\", target_os = \"linux\"))": [],
    "cfg(all(target_arch = \"x86\", target_env = \"gnu\", not(target_abi = \"llvm\"), not(windows_raw_dylib)))": ["@rules_rust//rust/platform:i686-unknown-linux-gnu"],
    "cfg(all(target_arch = \"x86\", target_env = \"msvc\", not(windows_raw_dylib)))": ["@rules_rust//rust/platform:i686-pc-windows-msvc"],
    "cfg(all(target_arch = \"x86_64\", target_env = \"gnu\", not(target_abi = \"llvm\"), not(windows_raw_dylib)))": ["@rules_rust//rust/platform:x86_64-unknown-linux-gnu", "@rules_rust//rust/platform:x86_64-unknown-nixos-gnu"],
    "cfg(any())": [],
    "cfg(any(target_arch = \"aarch64\", target_arch = \"x86\", target_arch = \"x86_64\"))": ["@rules_rust//rust/platform:aarch64-apple-darwin", "@rules_rust//rust/platform:aarch64-apple-ios", "@rules_rust//rust/platform:aarch64-apple-ios-macabi", "@rules_rust//rust/platform:aarch64-apple-ios-sim", "@rules_rust//rust/platform:aarch64-linux-android", "@rules_rust//rust/platform:aarch64-pc-windows-msvc", "@rules_rust//rust/platform:aarch64-unknown-fuchsia", "@rules_rust//rust/platform:aarch64-unknown-linux-gnu", "@rules_rust//rust/platform:aarch64-unknown-nixos-gnu", "@rules_rust//rust/platform:aarch64-unknown-nto-qnx710", "@rules_rust//rust/platform:aarch64-unknown-uefi", "@rules_rust//rust/platform:i686-apple-darwin", "@rules_rust//rust/platform:i686-linux-android", "@rules_rust//rust/platform:i686-pc-windows-msvc", "@rules_rust//rust/platform:i686-unknown-freebsd", "@rules_rust//rust/platform:i686-unknown-linux-gnu", "@rules_rust//rust/platform:x86_64-apple-darwin", "@rules_rust//rust/platform:x86_64-apple-ios", "@rules_rust//rust/platform:x86_64-apple-ios-macabi", "@rules_rust//rust/platform:x86_64-linux-android", "@rules_rust//rust/platform:x86_64-pc-windows-msvc", "@rules_rust//rust/platform:x86_64-unknown-freebsd", "@rules_rust//rust/platform:x86_64-unknown-fuchsia", "@rules_rust//rust/platform:x86_64-unknown-linux-gnu", "@rules_rust//rust/platform:x86_64-unknown-nixos-gnu", "@rules_rust//rust/platform:x86_64-unknown-none", "@rules_rust//rust/platform:x86_64-unknown-uefi"],
    "cfg(any(windows, target_os = \"cygwin\"))": ["@rules_rust//rust/platform:aarch64-pc-windows-msvc", "@rules_rust//rust/platform:i686-pc-windows-msvc", "@rules_rust//rust/platform:x86_64-pc-windows-msvc"],
    "cfg(not(all(windows, target_env = \"msvc\", not(target_vendor = \"uwp\"))))": ["@rules_rust//rust/platform:aarch64-apple-darwin", "@rules_rust//rust/platform:aarch64-apple-ios", "@rules_rust//rust/platform:aarch64-apple-ios-macabi", "@rules_rust//rust/platform:aarch64-apple-ios-sim", "@rules_rust//rust/platform:aarch64-linux-android", "@rules_rust//rust/platform:aarch64-unknown-fuchsia", "@rules_rust//rust/platform:aarch64-unknown-linux-gnu", "@rules_rust//rust/platform:aarch64-unknown-nixos-gnu", "@rules_rust//rust/platform:aarch64-unknown-nto-qnx710", "@rules_rust//rust/platform:aarch64-unknown-uefi", "@rules_rust//rust/platform:arm-unknown-linux-gnueabi", "@rules_rust//rust/platform:arm-unknown-linux-musleabi", "@rules_rust//rust/platform:armv7-linux-androideabi", "@rules_rust//rust/platform:armv7-unknown-linux-gnueabi", "@rules_rust//rust/platform:i686-apple-darwin", "@rules_rust//rust/platform:i686-linux-android", "@rules_rust//rust/platform:i686-unknown-freebsd", "@rules_rust//rust/platform:i686-unknown-linux-gnu", "@rules_rust//rust/platform:powerpc-unknown-linux-gnu", "@rules_rust//rust/platform:riscv32imc-unknown-none-elf", "@rules_rust//rust/platform:riscv64gc-unknown-linux-gnu", "@rules_rust//rust/platform:riscv64gc-unknown-none-elf", "@rules_rust//rust/platform:s390x-unknown-linux-gnu", "@rules_rust//rust/platform:thumbv6m-none-eabi", "@rules_rust//rust/platform:thumbv7em-none-eabi", "@rules_rust//rust/platform:thumbv7em-none-eabihf", "@rules_rust//rust/platform:thumbv8m.main-none-eabi", "@rules_rust//rust/platform:wasm32-unknown-emscripten", "@rules_rust//rust/platform:wasm32-unknown-unknown", "@rules_rust//rust/platform:wasm32-wasip1", "@rules_rust//rust/platform:wasm32-wasip1-threads", "@rules_rust//rust/platform:wasm32-wasip2", "@rules_rust//rust/platform:x86_64-apple-darwin", "@rules_rust//rust/platform:x86_64-apple-ios", "@rules_rust//rust/platform:x86_64-apple-ios-macabi", "@rules_rust//rust/platform:x86_64-linux-android", "@rules_rust//rust/platform:x86_64-unknown-freebsd", "@rules_rust//rust/platform:x86_64-unknown-fuchsia", "@rules_rust//rust/platform:x86_64-unknown-linux-gnu", "@rules_rust//rust/platform:x86_64-unknown-nixos-gnu", "@rules_rust//rust/platform:x86_64-unknown-none", "@rules_rust//rust/platform:x86_64-unknown-uefi"],
    "cfg(target_os = \"hermit\")": [],
    "cfg(target_os = \"wasi\")": ["@rules_rust//rust/platform:wasm32-wasip1", "@rules_rust//rust/platform:wasm32-wasip1-threads", "@rules_rust//rust/platform:wasm32-wasip2"],
    "cfg(unix)": ["@rules_rust//rust/platform:aarch64-apple-darwin", "@rules_rust//rust/platform:aarch64-apple-ios", "@rules_rust//rust/platform:aarch64-apple-ios-macabi", "@rules_rust//rust/platform:aarch64-apple-ios-sim", "@rules_rust//rust/platform:aarch64-linux-android", "@rules_rust//rust/platform:aarch64-unknown-fuchsia", "@rules_rust//rust/platform:aarch64-unknown-linux-gnu", "@rules_rust//rust/platform:aarch64-unknown-nixos-gnu", "@rules_rust//rust/platform:aarch64-unknown-nto-qnx710", "@rules_rust//rust/platform:arm-unknown-linux-gnueabi", "@rules_rust//rust/platform:arm-unknown-linux-musleabi", "@rules_rust//rust/platform:armv7-linux-androideabi", "@rules_rust//rust/platform:armv7-unknown-linux-gnueabi", "@rules_rust//rust/platform:i686-apple-darwin", "@rules_rust//rust/platform:i686-linux-android", "@rules_rust//rust/platform:i686-unknown-freebsd", "@rules_rust//rust/platform:i686-unknown-linux-gnu", "@rules_rust//rust/platform:powerpc-unknown-linux-gnu", "@rules_rust//rust/platform:riscv64gc-unknown-linux-gnu", "@rules_rust//rust/platform:s390x-unknown-linux-gnu", "@rules_rust//rust/platform:wasm32-unknown-emscripten", "@rules_rust//rust/platform:x86_64-apple-darwin", "@rules_rust//rust/platform:x86_64-apple-ios", "@rules_rust//rust/platform:x86_64-apple-ios-macabi", "@rules_rust//rust/platform:x86_64-linux-android", "@rules_rust//rust/platform:x86_64-unknown-freebsd", "@rules_rust//rust/platform:x86_64-unknown-fuchsia", "@rules_rust//rust/platform:x86_64-unknown-linux-gnu", "@rules_rust//rust/platform:x86_64-unknown-nixos-gnu"],
    "cfg(windows)": ["@rules_rust//rust/platform:aarch64-pc-windows-msvc", "@rules_rust//rust/platform:i686-pc-windows-msvc", "@rules_rust//rust/platform:x86_64-pc-windows-msvc"],
    "cfg(windows_raw_dylib)": [],
    "i686-apple-darwin": ["@rules_rust//rust/platform:i686-apple-darwin"],
    "i686-linux-android": ["@rules_rust//rust/platform:i686-linux-android"],
    "i686-pc-windows-gnullvm": [],
    "i686-pc-windows-msvc": ["@rules_rust//rust/platform:i686-pc-windows-msvc"],
    "i686-unknown-freebsd": ["@rules_rust//rust/platform:i686-unknown-freebsd"],
    "i686-unknown-linux-gnu": ["@rules_rust//rust/platform:i686-unknown-linux-gnu"],
    "powerpc-unknown-linux-gnu": ["@rules_rust//rust/platform:powerpc-unknown-linux-gnu"],
    "riscv32imc-unknown-none-elf": ["@rules_rust//rust/platform:riscv32imc-unknown-none-elf"],
    "riscv64gc-unknown-linux-gnu": ["@rules_rust//rust/platform:riscv64gc-unknown-linux-gnu"],
    "riscv64gc-unknown-none-elf": ["@rules_rust//rust/platform:riscv64gc-unknown-none-elf"],
    "s390x-unknown-linux-gnu": ["@rules_rust//rust/platform:s390x-unknown-linux-gnu"],
    "thumbv6m-none-eabi": ["@rules_rust//rust/platform:thumbv6m-none-eabi"],
    "thumbv7em-none-eabi": ["@rules_rust//rust/platform:thumbv7em-none-eabi"],
    "thumbv7em-none-eabihf": ["@rules_rust//rust/platform:thumbv7em-none-eabihf"],
    "thumbv8m.main-none-eabi": ["@rules_rust//rust/platform:thumbv8m.main-none-eabi"],
    "wasm32-unknown-emscripten": ["@rules_rust//rust/platform:wasm32-unknown-emscripten"],
    "wasm32-unknown-unknown": ["@rules_rust//rust/platform:wasm32-unknown-unknown"],
    "wasm32-wasip1": ["@rules_rust//rust/platform:wasm32-wasip1"],
    "wasm32-wasip1-threads": ["@rules_rust//rust/platform:wasm32-wasip1-threads"],
    "wasm32-wasip2": ["@rules_rust//rust/platform:wasm32-wasip2"],
    "x86_64-apple-darwin": ["@rules_rust//rust/platform:x86_64-apple-darwin"],
    "x86_64-apple-ios": ["@rules_rust//rust/platform:x86_64-apple-ios"],
    "x86_64-apple-ios-macabi": ["@rules_rust//rust/platform:x86_64-apple-ios-macabi"],
    "x86_64-linux-android": ["@rules_rust//rust/platform:x86_64-linux-android"],
    "x86_64-pc-windows-gnullvm": [],
    "x86_64-pc-windows-msvc": ["@rules_rust//rust/platform:x86_64-pc-windows-msvc"],
    "x86_64-unknown-freebsd": ["@rules_rust//rust/platform:x86_64-unknown-freebsd"],
    "x86_64-unknown-fuchsia": ["@rules_rust//rust/platform:x86_64-unknown-fuchsia"],
    "x86_64-unknown-linux-gnu": ["@rules_rust//rust/platform:x86_64-unknown-linux-gnu"],
    "x86_64-unknown-nixos-gnu": ["@rules_rust//rust/platform:x86_64-unknown-nixos-gnu"],
    "x86_64-unknown-none": ["@rules_rust//rust/platform:x86_64-unknown-none"],
    "x86_64-unknown-uefi": ["@rules_rust//rust/platform:x86_64-unknown-uefi"],
}

def crate_repositories():
    maybe(
        http_archive,
        name = "rules_typescript_crates__addr2line-0.25.1",
        sha256 = "1b5d307320b3181d6d7954e663bd7c774a838b8220fe0593c86d9fb09f498b4b",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/addr2line/0.25.1/download"],
        strip_prefix = "addr2line-0.25.1",
        build_file = Label("//oxc_cli/crates:BUILD.addr2line-0.25.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__adler2-2.0.1",
        sha256 = "320119579fcad9c21884f5c4861d16174d0e06250625266f50fe6898340abefa",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/adler2/2.0.1/download"],
        strip_prefix = "adler2-2.0.1",
        build_file = Label("//oxc_cli/crates:BUILD.adler2-2.0.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__allocator-api2-0.2.21",
        sha256 = "683d7910e743518b0e34f1186f92494becacb047c7b6bf616c96772180fef923",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/allocator-api2/0.2.21/download"],
        strip_prefix = "allocator-api2-0.2.21",
        build_file = Label("//oxc_cli/crates:BUILD.allocator-api2-0.2.21.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__anstream-0.6.21",
        sha256 = "43d5b281e737544384e969a5ccad3f1cdd24b48086a0fc1b2a5262a26b8f4f4a",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/anstream/0.6.21/download"],
        strip_prefix = "anstream-0.6.21",
        build_file = Label("//oxc_cli/crates:BUILD.anstream-0.6.21.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__anstyle-1.0.13",
        sha256 = "5192cca8006f1fd4f7237516f40fa183bb07f8fbdfedaa0036de5ea9b0b45e78",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/anstyle/1.0.13/download"],
        strip_prefix = "anstyle-1.0.13",
        build_file = Label("//oxc_cli/crates:BUILD.anstyle-1.0.13.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__anstyle-parse-0.2.7",
        sha256 = "4e7644824f0aa2c7b9384579234ef10eb7efb6a0deb83f9630a49594dd9c15c2",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/anstyle-parse/0.2.7/download"],
        strip_prefix = "anstyle-parse-0.2.7",
        build_file = Label("//oxc_cli/crates:BUILD.anstyle-parse-0.2.7.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__anstyle-query-1.1.5",
        sha256 = "40c48f72fd53cd289104fc64099abca73db4166ad86ea0b4341abe65af83dadc",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/anstyle-query/1.1.5/download"],
        strip_prefix = "anstyle-query-1.1.5",
        build_file = Label("//oxc_cli/crates:BUILD.anstyle-query-1.1.5.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__anstyle-wincon-3.0.11",
        sha256 = "291e6a250ff86cd4a820112fb8898808a366d8f9f58ce16d1f538353ad55747d",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/anstyle-wincon/3.0.11/download"],
        strip_prefix = "anstyle-wincon-3.0.11",
        build_file = Label("//oxc_cli/crates:BUILD.anstyle-wincon-3.0.11.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__autocfg-1.5.0",
        sha256 = "c08606f8c3cbf4ce6ec8e28fb0014a2c086708fe954eaa885384a6165172e7e8",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/autocfg/1.5.0/download"],
        strip_prefix = "autocfg-1.5.0",
        build_file = Label("//oxc_cli/crates:BUILD.autocfg-1.5.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__backtrace-0.3.76",
        sha256 = "bb531853791a215d7c62a30daf0dde835f381ab5de4589cfe7c649d2cbe92bd6",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/backtrace/0.3.76/download"],
        strip_prefix = "backtrace-0.3.76",
        build_file = Label("//oxc_cli/crates:BUILD.backtrace-0.3.76.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__backtrace-ext-0.2.1",
        sha256 = "537beee3be4a18fb023b570f80e3ae28003db9167a751266b259926e25539d50",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/backtrace-ext/0.2.1/download"],
        strip_prefix = "backtrace-ext-0.2.1",
        build_file = Label("//oxc_cli/crates:BUILD.backtrace-ext-0.2.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__base64-0.22.1",
        sha256 = "72b3254f16251a8381aa12e40e3c4d2f0199f8c6508fbecb9d91f575e0fbb8c6",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/base64/0.22.1/download"],
        strip_prefix = "base64-0.22.1",
        build_file = Label("//oxc_cli/crates:BUILD.base64-0.22.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__base64-simd-0.8.0",
        sha256 = "339abbe78e73178762e23bea9dfd08e697eb3f3301cd4be981c0f78ba5859195",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/base64-simd/0.8.0/download"],
        strip_prefix = "base64-simd-0.8.0",
        build_file = Label("//oxc_cli/crates:BUILD.base64-simd-0.8.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__bitflags-2.11.0",
        sha256 = "843867be96c8daad0d758b57df9392b6d8d271134fce549de6ce169ff98a92af",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/bitflags/2.11.0/download"],
        strip_prefix = "bitflags-2.11.0",
        build_file = Label("//oxc_cli/crates:BUILD.bitflags-2.11.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__block-buffer-0.10.4",
        sha256 = "3078c7629b62d3f0439517fa394996acacc5cbc91c5a20d8c658e77abd503a71",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/block-buffer/0.10.4/download"],
        strip_prefix = "block-buffer-0.10.4",
        build_file = Label("//oxc_cli/crates:BUILD.block-buffer-0.10.4.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__castaway-0.2.4",
        sha256 = "dec551ab6e7578819132c713a93c022a05d60159dc86e7a7050223577484c55a",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/castaway/0.2.4/download"],
        strip_prefix = "castaway-0.2.4",
        build_file = Label("//oxc_cli/crates:BUILD.castaway-0.2.4.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__cfg-if-1.0.4",
        sha256 = "9330f8b2ff13f34540b44e946ef35111825727b38d33286ef986142615121801",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/cfg-if/1.0.4/download"],
        strip_prefix = "cfg-if-1.0.4",
        build_file = Label("//oxc_cli/crates:BUILD.cfg-if-1.0.4.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__clap-4.5.60",
        sha256 = "2797f34da339ce31042b27d23607e051786132987f595b02ba4f6a6dffb7030a",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/clap/4.5.60/download"],
        strip_prefix = "clap-4.5.60",
        build_file = Label("//oxc_cli/crates:BUILD.clap-4.5.60.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__clap_builder-4.5.60",
        sha256 = "24a241312cea5059b13574bb9b3861cabf758b879c15190b37b6d6fd63ab6876",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/clap_builder/4.5.60/download"],
        strip_prefix = "clap_builder-4.5.60",
        build_file = Label("//oxc_cli/crates:BUILD.clap_builder-4.5.60.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__clap_derive-4.5.55",
        sha256 = "a92793da1a46a5f2a02a6f4c46c6496b28c43638adea8306fcb0caa1634f24e5",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/clap_derive/4.5.55/download"],
        strip_prefix = "clap_derive-4.5.55",
        build_file = Label("//oxc_cli/crates:BUILD.clap_derive-4.5.55.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__clap_lex-1.0.0",
        sha256 = "3a822ea5bc7590f9d40f1ba12c0dc3c2760f3482c6984db1573ad11031420831",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/clap_lex/1.0.0/download"],
        strip_prefix = "clap_lex-1.0.0",
        build_file = Label("//oxc_cli/crates:BUILD.clap_lex-1.0.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__cobs-0.3.0",
        sha256 = "0fa961b519f0b462e3a3b4a34b64d119eeaca1d59af726fe450bbba07a9fc0a1",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/cobs/0.3.0/download"],
        strip_prefix = "cobs-0.3.0",
        build_file = Label("//oxc_cli/crates:BUILD.cobs-0.3.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__colorchoice-1.0.4",
        sha256 = "b05b61dc5112cbb17e4b6cd61790d9845d13888356391624cbe7e41efeac1e75",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/colorchoice/1.0.4/download"],
        strip_prefix = "colorchoice-1.0.4",
        build_file = Label("//oxc_cli/crates:BUILD.colorchoice-1.0.4.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__compact_str-0.9.0",
        sha256 = "3fdb1325a1cece981e8a296ab8f0f9b63ae357bd0784a9faaf548cc7b480707a",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/compact_str/0.9.0/download"],
        strip_prefix = "compact_str-0.9.0",
        build_file = Label("//oxc_cli/crates:BUILD.compact_str-0.9.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__cow-utils-0.1.3",
        sha256 = "417bef24afe1460300965a25ff4a24b8b45ad011948302ec221e8a0a81eb2c79",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/cow-utils/0.1.3/download"],
        strip_prefix = "cow-utils-0.1.3",
        build_file = Label("//oxc_cli/crates:BUILD.cow-utils-0.1.3.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__cpufeatures-0.2.17",
        sha256 = "59ed5838eebb26a2bb2e58f6d5b5316989ae9d08bab10e0e6d103e656d1b0280",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/cpufeatures/0.2.17/download"],
        strip_prefix = "cpufeatures-0.2.17",
        build_file = Label("//oxc_cli/crates:BUILD.cpufeatures-0.2.17.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__crc32fast-1.5.0",
        sha256 = "9481c1c90cbf2ac953f07c8d4a58aa3945c425b7185c9154d67a65e4230da511",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/crc32fast/1.5.0/download"],
        strip_prefix = "crc32fast-1.5.0",
        build_file = Label("//oxc_cli/crates:BUILD.crc32fast-1.5.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__crossbeam-deque-0.8.6",
        sha256 = "9dd111b7b7f7d55b72c0a6ae361660ee5853c9af73f70c3c2ef6858b950e2e51",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/crossbeam-deque/0.8.6/download"],
        strip_prefix = "crossbeam-deque-0.8.6",
        build_file = Label("//oxc_cli/crates:BUILD.crossbeam-deque-0.8.6.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__crossbeam-epoch-0.9.18",
        sha256 = "5b82ac4a3c2ca9c3460964f020e1402edd5753411d7737aa39c3714ad1b5420e",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/crossbeam-epoch/0.9.18/download"],
        strip_prefix = "crossbeam-epoch-0.9.18",
        build_file = Label("//oxc_cli/crates:BUILD.crossbeam-epoch-0.9.18.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__crossbeam-utils-0.8.21",
        sha256 = "d0a5c400df2834b80a4c3327b3aad3a4c4cd4de0629063962b03235697506a28",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/crossbeam-utils/0.8.21/download"],
        strip_prefix = "crossbeam-utils-0.8.21",
        build_file = Label("//oxc_cli/crates:BUILD.crossbeam-utils-0.8.21.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__crypto-common-0.1.7",
        sha256 = "78c8292055d1c1df0cce5d180393dc8cce0abec0a7102adb6c7b1eef6016d60a",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/crypto-common/0.1.7/download"],
        strip_prefix = "crypto-common-0.1.7",
        build_file = Label("//oxc_cli/crates:BUILD.crypto-common-0.1.7.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__digest-0.10.7",
        sha256 = "9ed9a281f7bc9b7576e61468ba615a66a5c8cfdff42420a70aa82701a3b1e292",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/digest/0.10.7/download"],
        strip_prefix = "digest-0.10.7",
        build_file = Label("//oxc_cli/crates:BUILD.digest-0.10.7.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__dragonbox_ecma-0.1.12",
        sha256 = "fd8e701084c37e7ef62d3f9e453b618130cbc0ef3573847785952a3ac3f746bf",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/dragonbox_ecma/0.1.12/download"],
        strip_prefix = "dragonbox_ecma-0.1.12",
        build_file = Label("//oxc_cli/crates:BUILD.dragonbox_ecma-0.1.12.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__either-1.15.0",
        sha256 = "48c757948c5ede0e46177b7add2e67155f70e33c07fea8284df6576da70b3719",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/either/1.15.0/download"],
        strip_prefix = "either-1.15.0",
        build_file = Label("//oxc_cli/crates:BUILD.either-1.15.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__embedded-io-0.4.0",
        sha256 = "ef1a6892d9eef45c8fa6b9e0086428a2cca8491aca8f787c534a3d6d0bcb3ced",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/embedded-io/0.4.0/download"],
        strip_prefix = "embedded-io-0.4.0",
        build_file = Label("//oxc_cli/crates:BUILD.embedded-io-0.4.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__embedded-io-0.6.1",
        sha256 = "edd0f118536f44f5ccd48bcb8b111bdc3de888b58c74639dfb034a357d0f206d",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/embedded-io/0.6.1/download"],
        strip_prefix = "embedded-io-0.6.1",
        build_file = Label("//oxc_cli/crates:BUILD.embedded-io-0.6.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__equivalent-1.0.2",
        sha256 = "877a4ace8713b0bcf2a4e7eec82529c029f1d0619886d18145fea96c3ffe5c0f",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/equivalent/1.0.2/download"],
        strip_prefix = "equivalent-1.0.2",
        build_file = Label("//oxc_cli/crates:BUILD.equivalent-1.0.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__errno-0.3.14",
        sha256 = "39cab71617ae0d63f51a36d69f866391735b51691dbda63cf6f96d042b63efeb",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/errno/0.3.14/download"],
        strip_prefix = "errno-0.3.14",
        build_file = Label("//oxc_cli/crates:BUILD.errno-0.3.14.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__fastrand-2.3.0",
        sha256 = "37909eebbb50d72f9059c3b6d82c0463f2ff062c9e95845c43a6c9c0355411be",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/fastrand/2.3.0/download"],
        strip_prefix = "fastrand-2.3.0",
        build_file = Label("//oxc_cli/crates:BUILD.fastrand-2.3.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__flate2-1.1.9",
        sha256 = "843fba2746e448b37e26a819579957415c8cef339bf08564fe8b7ddbd959573c",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/flate2/1.1.9/download"],
        strip_prefix = "flate2-1.1.9",
        build_file = Label("//oxc_cli/crates:BUILD.flate2-1.1.9.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__generic-array-0.14.7",
        sha256 = "85649ca51fd72272d7821adaf274ad91c288277713d9c18820d8499a7ff69e9a",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/generic-array/0.14.7/download"],
        strip_prefix = "generic-array-0.14.7",
        build_file = Label("//oxc_cli/crates:BUILD.generic-array-0.14.7.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__gimli-0.32.3",
        sha256 = "e629b9b98ef3dd8afe6ca2bd0f89306cec16d43d907889945bc5d6687f2f13c7",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/gimli/0.32.3/download"],
        strip_prefix = "gimli-0.32.3",
        build_file = Label("//oxc_cli/crates:BUILD.gimli-0.32.3.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__hashbrown-0.16.1",
        sha256 = "841d1cc9bed7f9236f321df977030373f4a4163ae1a7dbfe1a51a2c1a51d9100",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/hashbrown/0.16.1/download"],
        strip_prefix = "hashbrown-0.16.1",
        build_file = Label("//oxc_cli/crates:BUILD.hashbrown-0.16.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__heck-0.5.0",
        sha256 = "2304e00983f87ffb38b55b444b5e3b60a884b5d30c0fca7d82fe33449bbe55ea",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/heck/0.5.0/download"],
        strip_prefix = "heck-0.5.0",
        build_file = Label("//oxc_cli/crates:BUILD.heck-0.5.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__indexmap-2.13.0",
        sha256 = "7714e70437a7dc3ac8eb7e6f8df75fd8eb422675fc7678aff7364301092b1017",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/indexmap/2.13.0/download"],
        strip_prefix = "indexmap-2.13.0",
        build_file = Label("//oxc_cli/crates:BUILD.indexmap-2.13.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__is_ci-1.2.0",
        sha256 = "7655c9839580ee829dfacba1d1278c2b7883e50a277ff7541299489d6bdfdc45",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/is_ci/1.2.0/download"],
        strip_prefix = "is_ci-1.2.0",
        build_file = Label("//oxc_cli/crates:BUILD.is_ci-1.2.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__is_terminal_polyfill-1.70.2",
        sha256 = "a6cb138bb79a146c1bd460005623e142ef0181e3d0219cb493e02f7d08a35695",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/is_terminal_polyfill/1.70.2/download"],
        strip_prefix = "is_terminal_polyfill-1.70.2",
        build_file = Label("//oxc_cli/crates:BUILD.is_terminal_polyfill-1.70.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__itertools-0.14.0",
        sha256 = "2b192c782037fadd9cfa75548310488aabdbf3d2da73885b31bd0abd03351285",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/itertools/0.14.0/download"],
        strip_prefix = "itertools-0.14.0",
        build_file = Label("//oxc_cli/crates:BUILD.itertools-0.14.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__itoa-1.0.17",
        sha256 = "92ecc6618181def0457392ccd0ee51198e065e016d1d527a7ac1b6dc7c1f09d2",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/itoa/1.0.17/download"],
        strip_prefix = "itoa-1.0.17",
        build_file = Label("//oxc_cli/crates:BUILD.itoa-1.0.17.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__json-escape-simd-3.0.1",
        sha256 = "a3c2a6c0b4b5637c41719973ef40c6a1cf564f9db6958350de6193fbee9c23f5",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/json-escape-simd/3.0.1/download"],
        strip_prefix = "json-escape-simd-3.0.1",
        build_file = Label("//oxc_cli/crates:BUILD.json-escape-simd-3.0.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__libc-0.2.183",
        sha256 = "b5b646652bf6661599e1da8901b3b9522896f01e736bad5f723fe7a3a27f899d",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/libc/0.2.183/download"],
        strip_prefix = "libc-0.2.183",
        build_file = Label("//oxc_cli/crates:BUILD.libc-0.2.183.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__linux-raw-sys-0.12.1",
        sha256 = "32a66949e030da00e8c7d4434b251670a91556f4144941d37452769c25d58a53",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/linux-raw-sys/0.12.1/download"],
        strip_prefix = "linux-raw-sys-0.12.1",
        build_file = Label("//oxc_cli/crates:BUILD.linux-raw-sys-0.12.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__memchr-2.8.0",
        sha256 = "f8ca58f447f06ed17d5fc4043ce1b10dd205e060fb3ce5b979b8ed8e59ff3f79",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/memchr/2.8.0/download"],
        strip_prefix = "memchr-2.8.0",
        build_file = Label("//oxc_cli/crates:BUILD.memchr-2.8.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__miette-7.6.0",
        sha256 = "5f98efec8807c63c752b5bd61f862c165c115b0a35685bdcfd9238c7aeb592b7",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/miette/7.6.0/download"],
        strip_prefix = "miette-7.6.0",
        build_file = Label("//oxc_cli/crates:BUILD.miette-7.6.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__miette-derive-7.6.0",
        sha256 = "db5b29714e950dbb20d5e6f74f9dcec4edbcc1067bb7f8ed198c097b8c1a818b",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/miette-derive/7.6.0/download"],
        strip_prefix = "miette-derive-7.6.0",
        build_file = Label("//oxc_cli/crates:BUILD.miette-derive-7.6.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__miniz_oxide-0.8.9",
        sha256 = "1fa76a2c86f704bdb222d66965fb3d63269ce38518b83cb0575fca855ebb6316",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/miniz_oxide/0.8.9/download"],
        strip_prefix = "miniz_oxide-0.8.9",
        build_file = Label("//oxc_cli/crates:BUILD.miniz_oxide-0.8.9.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__nonmax-0.5.5",
        sha256 = "610a5acd306ec67f907abe5567859a3c693fb9886eb1f012ab8f2a47bef3db51",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/nonmax/0.5.5/download"],
        strip_prefix = "nonmax-0.5.5",
        build_file = Label("//oxc_cli/crates:BUILD.nonmax-0.5.5.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__num-bigint-0.4.6",
        sha256 = "a5e44f723f1133c9deac646763579fdb3ac745e418f2a7af9cd0c431da1f20b9",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/num-bigint/0.4.6/download"],
        strip_prefix = "num-bigint-0.4.6",
        build_file = Label("//oxc_cli/crates:BUILD.num-bigint-0.4.6.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__num-integer-0.1.46",
        sha256 = "7969661fd2958a5cb096e56c8e1ad0444ac2bbcd0061bd28660485a44879858f",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/num-integer/0.1.46/download"],
        strip_prefix = "num-integer-0.1.46",
        build_file = Label("//oxc_cli/crates:BUILD.num-integer-0.1.46.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__num-traits-0.2.19",
        sha256 = "071dfc062690e90b734c0b2273ce72ad0ffa95f0c74596bc250dcfd960262841",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/num-traits/0.2.19/download"],
        strip_prefix = "num-traits-0.2.19",
        build_file = Label("//oxc_cli/crates:BUILD.num-traits-0.2.19.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__object-0.37.3",
        sha256 = "ff76201f031d8863c38aa7f905eca4f53abbfa15f609db4277d44cd8938f33fe",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/object/0.37.3/download"],
        strip_prefix = "object-0.37.3",
        build_file = Label("//oxc_cli/crates:BUILD.object-0.37.3.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__once_cell_polyfill-1.70.2",
        sha256 = "384b8ab6d37215f3c5301a95a4accb5d64aa607f1fcb26a11b5303878451b4fe",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/once_cell_polyfill/1.70.2/download"],
        strip_prefix = "once_cell_polyfill-1.70.2",
        build_file = Label("//oxc_cli/crates:BUILD.once_cell_polyfill-1.70.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__outref-0.5.2",
        sha256 = "1a80800c0488c3a21695ea981a54918fbb37abf04f4d0720c453632255e2ff0e",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/outref/0.5.2/download"],
        strip_prefix = "outref-0.5.2",
        build_file = Label("//oxc_cli/crates:BUILD.outref-0.5.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__owo-colors-4.3.0",
        sha256 = "d211803b9b6b570f68772237e415a029d5a50c65d382910b879fb19d3271f94d",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/owo-colors/4.3.0/download"],
        strip_prefix = "owo-colors-4.3.0",
        build_file = Label("//oxc_cli/crates:BUILD.owo-colors-4.3.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc-browserslist-3.0.0",
        sha256 = "bc15cd06df6b0464b763ec97a511527047350a6bfd93daf8ac82fedf21050083",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc-browserslist/3.0.0/download"],
        strip_prefix = "oxc-browserslist-3.0.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc-browserslist-3.0.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc-miette-2.7.0",
        sha256 = "60a7ba54c704edefead1f44e9ef09c43e5cfae666bdc33516b066011f0e6ebf7",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc-miette/2.7.0/download"],
        strip_prefix = "oxc-miette-2.7.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc-miette-2.7.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc-miette-derive-2.7.0",
        sha256 = "d4faecb54d0971f948fbc1918df69b26007e6f279a204793669542e1e8b75eb3",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc-miette-derive/2.7.0/download"],
        strip_prefix = "oxc-miette-derive-2.7.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc-miette-derive-2.7.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_allocator-0.117.0",
        sha256 = "97b44277218c002c09167474648a478d3d29a29095ef8950ec9f1fac016c62d7",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_allocator/0.117.0/download"],
        strip_prefix = "oxc_allocator-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_allocator-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_ast-0.117.0",
        sha256 = "e4222e4e7a1ab01b2a20420a5a65798377a748ea37ee7ece4d7a6b733f86eb61",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_ast/0.117.0/download"],
        strip_prefix = "oxc_ast-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_ast-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_ast_macros-0.117.0",
        sha256 = "8e65a38ae589e284dd45a85008024f04aa680e9ddf1321c163cf7f187c805e91",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_ast_macros/0.117.0/download"],
        strip_prefix = "oxc_ast_macros-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_ast_macros-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_ast_visit-0.117.0",
        sha256 = "7fddbcd453c55d11995a55f5b1b0dec2768f7e578eb0a7fdcf17d7724c2b45e2",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_ast_visit/0.117.0/download"],
        strip_prefix = "oxc_ast_visit-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_ast_visit-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_codegen-0.117.0",
        sha256 = "62ac61963e2af6c1d1b2fd8716bef2657d9470a1a9a6527c0a417cffedf796cd",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_codegen/0.117.0/download"],
        strip_prefix = "oxc_codegen-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_codegen-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_compat-0.117.0",
        sha256 = "5e2fc6f1baab710dd63a9a1e973c65e4d4e42ecba8f8ce00a6e3f9ace5ff6d15",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_compat/0.117.0/download"],
        strip_prefix = "oxc_compat-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_compat-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_data_structures-0.117.0",
        sha256 = "f53bed71cad192596aee8f87f6d6bc2a38a4f898255a69b1d41da1968b9b2c6f",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_data_structures/0.117.0/download"],
        strip_prefix = "oxc_data_structures-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_data_structures-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_diagnostics-0.117.0",
        sha256 = "1a2d2491c0a1ea29a83abe645424f85c64b5c825f60e5304a453e4314a8b6d88",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_diagnostics/0.117.0/download"],
        strip_prefix = "oxc_diagnostics-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_diagnostics-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_ecmascript-0.117.0",
        sha256 = "71b23b64fa8c4a84b1406de383c4666366c9f54ffb9cb11a63b8d7433950460a",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_ecmascript/0.117.0/download"],
        strip_prefix = "oxc_ecmascript-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_ecmascript-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_estree-0.117.0",
        sha256 = "a47515ead44bc8beec1ae1514f10ecca63cde043da167c0395dc914f098ea5d2",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_estree/0.117.0/download"],
        strip_prefix = "oxc_estree-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_estree-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_index-4.1.0",
        sha256 = "eb3e6120999627ec9703025eab7c9f410ebb7e95557632a8902ca48210416c2b",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_index/4.1.0/download"],
        strip_prefix = "oxc_index-4.1.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_index-4.1.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_isolated_declarations-0.117.0",
        sha256 = "af05a2c3fd445630bb05595d6367e18aea4a943177dd155bc4e7e57b9fa9da6e",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_isolated_declarations/0.117.0/download"],
        strip_prefix = "oxc_isolated_declarations-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_isolated_declarations-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_parser-0.117.0",
        sha256 = "3278d4f34d01cdaf85a2391d7b12daba1d95c20c1ff2ac9316d3c28f36353e4e",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_parser/0.117.0/download"],
        strip_prefix = "oxc_parser-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_parser-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_regular_expression-0.117.0",
        sha256 = "f3d680252672b22c24abbaf6e401eace0be9f53072a03411936204625ff349d0",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_regular_expression/0.117.0/download"],
        strip_prefix = "oxc_regular_expression-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_regular_expression-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_semantic-0.117.0",
        sha256 = "208725f572872b1d53d3d734f959eada9f3b93ca9f64381a625d4e55ec6dea19",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_semantic/0.117.0/download"],
        strip_prefix = "oxc_semantic-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_semantic-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_sourcemap-6.0.2",
        sha256 = "c7f89482522f3cd820817d48ee4ade5b10822060d6e5e4d419f05f6d8bd29d70",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_sourcemap/6.0.2/download"],
        strip_prefix = "oxc_sourcemap-6.0.2",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_sourcemap-6.0.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_span-0.117.0",
        sha256 = "b6eb1bd62de89fb0c646bfb053b72370750fab43a84ebe09ad97cfa020712314",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_span/0.117.0/download"],
        strip_prefix = "oxc_span-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_span-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_str-0.117.0",
        sha256 = "2e65cbfb06ecbae07e0da931815b6b03ade886d016302c400bda7dc0a2f600d3",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_str/0.117.0/download"],
        strip_prefix = "oxc_str-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_str-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_syntax-0.117.0",
        sha256 = "e0f1617f0aa890517fb61ffa1d2d73a8497aca52e84ef6f027fad1e93250eccc",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_syntax/0.117.0/download"],
        strip_prefix = "oxc_syntax-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_syntax-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_transformer-0.117.0",
        sha256 = "a57e160e5a2df719b7cfcd23f2d291bfdbbc887ec3bd0d7147214e7aa3a7b0a6",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_transformer/0.117.0/download"],
        strip_prefix = "oxc_transformer-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_transformer-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__oxc_traverse-0.117.0",
        sha256 = "74dabd9c79380a5b11b020eed0affc488f9ceeb6557fd81610653998d30af583",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/oxc_traverse/0.117.0/download"],
        strip_prefix = "oxc_traverse-0.117.0",
        build_file = Label("//oxc_cli/crates:BUILD.oxc_traverse-0.117.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__percent-encoding-2.3.2",
        sha256 = "9b4f627cb1b25917193a259e49bdad08f671f8d9708acfd5fe0a8c1455d87220",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/percent-encoding/2.3.2/download"],
        strip_prefix = "percent-encoding-2.3.2",
        build_file = Label("//oxc_cli/crates:BUILD.percent-encoding-2.3.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__phf-0.13.1",
        sha256 = "c1562dc717473dbaa4c1f85a36410e03c047b2e7df7f45ee938fbef64ae7fadf",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/phf/0.13.1/download"],
        strip_prefix = "phf-0.13.1",
        build_file = Label("//oxc_cli/crates:BUILD.phf-0.13.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__phf_generator-0.13.1",
        sha256 = "135ace3a761e564ec88c03a77317a7c6b80bb7f7135ef2544dbe054243b89737",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/phf_generator/0.13.1/download"],
        strip_prefix = "phf_generator-0.13.1",
        build_file = Label("//oxc_cli/crates:BUILD.phf_generator-0.13.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__phf_macros-0.13.1",
        sha256 = "812f032b54b1e759ccd5f8b6677695d5268c588701effba24601f6932f8269ef",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/phf_macros/0.13.1/download"],
        strip_prefix = "phf_macros-0.13.1",
        build_file = Label("//oxc_cli/crates:BUILD.phf_macros-0.13.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__phf_shared-0.13.1",
        sha256 = "e57fef6bc5981e38c2ce2d63bfa546861309f875b8a75f092d1d54ae2d64f266",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/phf_shared/0.13.1/download"],
        strip_prefix = "phf_shared-0.13.1",
        build_file = Label("//oxc_cli/crates:BUILD.phf_shared-0.13.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__postcard-1.1.3",
        sha256 = "6764c3b5dd454e283a30e6dfe78e9b31096d9e32036b5d1eaac7a6119ccb9a24",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/postcard/1.1.3/download"],
        strip_prefix = "postcard-1.1.3",
        build_file = Label("//oxc_cli/crates:BUILD.postcard-1.1.3.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__proc-macro2-1.0.106",
        sha256 = "8fd00f0bb2e90d81d1044c2b32617f68fcb9fa3bb7640c23e9c748e53fb30934",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/proc-macro2/1.0.106/download"],
        strip_prefix = "proc-macro2-1.0.106",
        build_file = Label("//oxc_cli/crates:BUILD.proc-macro2-1.0.106.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__quote-1.0.45",
        sha256 = "41f2619966050689382d2b44f664f4bc593e129785a36d6ee376ddf37259b924",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/quote/1.0.45/download"],
        strip_prefix = "quote-1.0.45",
        build_file = Label("//oxc_cli/crates:BUILD.quote-1.0.45.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__rayon-1.11.0",
        sha256 = "368f01d005bf8fd9b1206fb6fa653e6c4a81ceb1466406b81792d87c5677a58f",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/rayon/1.11.0/download"],
        strip_prefix = "rayon-1.11.0",
        build_file = Label("//oxc_cli/crates:BUILD.rayon-1.11.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__rayon-core-1.13.0",
        sha256 = "22e18b0f0062d30d4230b2e85ff77fdfe4326feb054b9783a3460d8435c8ab91",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/rayon-core/1.13.0/download"],
        strip_prefix = "rayon-core-1.13.0",
        build_file = Label("//oxc_cli/crates:BUILD.rayon-core-1.13.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__ropey-1.6.1",
        sha256 = "93411e420bcd1a75ddd1dc3caf18c23155eda2c090631a85af21ba19e97093b5",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/ropey/1.6.1/download"],
        strip_prefix = "ropey-1.6.1",
        build_file = Label("//oxc_cli/crates:BUILD.ropey-1.6.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__rustc-demangle-0.1.27",
        sha256 = "b50b8869d9fc858ce7266cce0194bd74df58b9d0e3f6df3a9fc8eb470d95c09d",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/rustc-demangle/0.1.27/download"],
        strip_prefix = "rustc-demangle-0.1.27",
        build_file = Label("//oxc_cli/crates:BUILD.rustc-demangle-0.1.27.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__rustc-hash-2.1.1",
        sha256 = "357703d41365b4b27c590e3ed91eabb1b663f07c4c084095e60cbed4362dff0d",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/rustc-hash/2.1.1/download"],
        strip_prefix = "rustc-hash-2.1.1",
        build_file = Label("//oxc_cli/crates:BUILD.rustc-hash-2.1.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__rustix-1.1.4",
        sha256 = "b6fe4565b9518b83ef4f91bb47ce29620ca828bd32cb7e408f0062e9930ba190",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/rustix/1.1.4/download"],
        strip_prefix = "rustix-1.1.4",
        build_file = Label("//oxc_cli/crates:BUILD.rustix-1.1.4.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__rustversion-1.0.22",
        sha256 = "b39cdef0fa800fc44525c84ccb54a029961a8215f9619753635a9c0d2538d46d",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/rustversion/1.0.22/download"],
        strip_prefix = "rustversion-1.0.22",
        build_file = Label("//oxc_cli/crates:BUILD.rustversion-1.0.22.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__ryu-1.0.23",
        sha256 = "9774ba4a74de5f7b1c1451ed6cd5285a32eddb5cccb8cc655a4e50009e06477f",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/ryu/1.0.23/download"],
        strip_prefix = "ryu-1.0.23",
        build_file = Label("//oxc_cli/crates:BUILD.ryu-1.0.23.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__self_cell-1.2.2",
        sha256 = "b12e76d157a900eb52e81bc6e9f3069344290341720e9178cde2407113ac8d89",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/self_cell/1.2.2/download"],
        strip_prefix = "self_cell-1.2.2",
        build_file = Label("//oxc_cli/crates:BUILD.self_cell-1.2.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__seq-macro-0.3.6",
        sha256 = "1bc711410fbe7399f390ca1c3b60ad0f53f80e95c5eb935e52268a0e2cd49acc",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/seq-macro/0.3.6/download"],
        strip_prefix = "seq-macro-0.3.6",
        build_file = Label("//oxc_cli/crates:BUILD.seq-macro-0.3.6.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__serde-1.0.228",
        sha256 = "9a8e94ea7f378bd32cbbd37198a4a91436180c5bb472411e48b5ec2e2124ae9e",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/serde/1.0.228/download"],
        strip_prefix = "serde-1.0.228",
        build_file = Label("//oxc_cli/crates:BUILD.serde-1.0.228.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__serde_core-1.0.228",
        sha256 = "41d385c7d4ca58e59fc732af25c3983b67ac852c1a25000afe1175de458b67ad",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/serde_core/1.0.228/download"],
        strip_prefix = "serde_core-1.0.228",
        build_file = Label("//oxc_cli/crates:BUILD.serde_core-1.0.228.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__serde_derive-1.0.228",
        sha256 = "d540f220d3187173da220f885ab66608367b6574e925011a9353e4badda91d79",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/serde_derive/1.0.228/download"],
        strip_prefix = "serde_derive-1.0.228",
        build_file = Label("//oxc_cli/crates:BUILD.serde_derive-1.0.228.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__serde_json-1.0.149",
        sha256 = "83fc039473c5595ace860d8c4fafa220ff474b3fc6bfdb4293327f1a37e94d86",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/serde_json/1.0.149/download"],
        strip_prefix = "serde_json-1.0.149",
        build_file = Label("//oxc_cli/crates:BUILD.serde_json-1.0.149.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__sha1-0.10.6",
        sha256 = "e3bf829a2d51ab4a5ddf1352d8470c140cadc8301b2ae1789db023f01cedd6ba",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/sha1/0.10.6/download"],
        strip_prefix = "sha1-0.10.6",
        build_file = Label("//oxc_cli/crates:BUILD.sha1-0.10.6.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__simd-adler32-0.3.8",
        sha256 = "e320a6c5ad31d271ad523dcf3ad13e2767ad8b1cb8f047f75a8aeaf8da139da2",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/simd-adler32/0.3.8/download"],
        strip_prefix = "simd-adler32-0.3.8",
        build_file = Label("//oxc_cli/crates:BUILD.simd-adler32-0.3.8.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__siphasher-1.0.2",
        sha256 = "b2aa850e253778c88a04c3d7323b043aeda9d3e30d5971937c1855769763678e",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/siphasher/1.0.2/download"],
        strip_prefix = "siphasher-1.0.2",
        build_file = Label("//oxc_cli/crates:BUILD.siphasher-1.0.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__smallvec-1.15.1",
        sha256 = "67b1b7a3b5fe4f1376887184045fcf45c69e92af734b7aaddc05fb777b6fbd03",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/smallvec/1.15.1/download"],
        strip_prefix = "smallvec-1.15.1",
        build_file = Label("//oxc_cli/crates:BUILD.smallvec-1.15.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__smawk-0.3.2",
        sha256 = "b7c388c1b5e93756d0c740965c41e8822f866621d41acbdf6336a6a168f8840c",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/smawk/0.3.2/download"],
        strip_prefix = "smawk-0.3.2",
        build_file = Label("//oxc_cli/crates:BUILD.smawk-0.3.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__static_assertions-1.1.0",
        sha256 = "a2eb9349b6444b326872e140eb1cf5e7c522154d69e7a0ffb0fb81c06b37543f",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/static_assertions/1.1.0/download"],
        strip_prefix = "static_assertions-1.1.0",
        build_file = Label("//oxc_cli/crates:BUILD.static_assertions-1.1.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__str_indices-0.4.4",
        sha256 = "d08889ec5408683408db66ad89e0e1f93dff55c73a4ccc71c427d5b277ee47e6",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/str_indices/0.4.4/download"],
        strip_prefix = "str_indices-0.4.4",
        build_file = Label("//oxc_cli/crates:BUILD.str_indices-0.4.4.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__strsim-0.11.1",
        sha256 = "7da8b5736845d9f2fcb837ea5d9e2628564b3b043a70948a3f0b778838c5fb4f",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/strsim/0.11.1/download"],
        strip_prefix = "strsim-0.11.1",
        build_file = Label("//oxc_cli/crates:BUILD.strsim-0.11.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__supports-color-3.0.2",
        sha256 = "c64fc7232dd8d2e4ac5ce4ef302b1d81e0b80d055b9d77c7c4f51f6aa4c867d6",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/supports-color/3.0.2/download"],
        strip_prefix = "supports-color-3.0.2",
        build_file = Label("//oxc_cli/crates:BUILD.supports-color-3.0.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__supports-hyperlinks-3.2.0",
        sha256 = "e396b6523b11ccb83120b115a0b7366de372751aa6edf19844dfb13a6af97e91",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/supports-hyperlinks/3.2.0/download"],
        strip_prefix = "supports-hyperlinks-3.2.0",
        build_file = Label("//oxc_cli/crates:BUILD.supports-hyperlinks-3.2.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__supports-unicode-3.0.0",
        sha256 = "b7401a30af6cb5818bb64852270bb722533397edcfc7344954a38f420819ece2",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/supports-unicode/3.0.0/download"],
        strip_prefix = "supports-unicode-3.0.0",
        build_file = Label("//oxc_cli/crates:BUILD.supports-unicode-3.0.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__syn-2.0.117",
        sha256 = "e665b8803e7b1d2a727f4023456bbbbe74da67099c585258af0ad9c5013b9b99",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/syn/2.0.117/download"],
        strip_prefix = "syn-2.0.117",
        build_file = Label("//oxc_cli/crates:BUILD.syn-2.0.117.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__terminal_size-0.4.3",
        sha256 = "60b8cb979cb11c32ce1603f8137b22262a9d131aaa5c37b5678025f22b8becd0",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/terminal_size/0.4.3/download"],
        strip_prefix = "terminal_size-0.4.3",
        build_file = Label("//oxc_cli/crates:BUILD.terminal_size-0.4.3.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__textwrap-0.16.2",
        sha256 = "c13547615a44dc9c452a8a534638acdf07120d4b6847c8178705da06306a3057",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/textwrap/0.16.2/download"],
        strip_prefix = "textwrap-0.16.2",
        build_file = Label("//oxc_cli/crates:BUILD.textwrap-0.16.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__thiserror-2.0.18",
        sha256 = "4288b5bcbc7920c07a1149a35cf9590a2aa808e0bc1eafaade0b80947865fbc4",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/thiserror/2.0.18/download"],
        strip_prefix = "thiserror-2.0.18",
        build_file = Label("//oxc_cli/crates:BUILD.thiserror-2.0.18.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__thiserror-impl-2.0.18",
        sha256 = "ebc4ee7f67670e9b64d05fa4253e753e016c6c95ff35b89b7941d6b856dec1d5",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/thiserror-impl/2.0.18/download"],
        strip_prefix = "thiserror-impl-2.0.18",
        build_file = Label("//oxc_cli/crates:BUILD.thiserror-impl-2.0.18.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__typenum-1.19.0",
        sha256 = "562d481066bde0658276a35467c4af00bdc6ee726305698a55b86e61d7ad82bb",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/typenum/1.19.0/download"],
        strip_prefix = "typenum-1.19.0",
        build_file = Label("//oxc_cli/crates:BUILD.typenum-1.19.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__unicode-id-start-1.4.0",
        sha256 = "81b79ad29b5e19de4260020f8919b443b2ef0277d242ce532ec7b7a2cc8b6007",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/unicode-id-start/1.4.0/download"],
        strip_prefix = "unicode-id-start-1.4.0",
        build_file = Label("//oxc_cli/crates:BUILD.unicode-id-start-1.4.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__unicode-ident-1.0.24",
        sha256 = "e6e4313cd5fcd3dad5cafa179702e2b244f760991f45397d14d4ebf38247da75",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/unicode-ident/1.0.24/download"],
        strip_prefix = "unicode-ident-1.0.24",
        build_file = Label("//oxc_cli/crates:BUILD.unicode-ident-1.0.24.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__unicode-linebreak-0.1.5",
        sha256 = "3b09c83c3c29d37506a3e260c08c03743a6bb66a9cd432c6934ab501a190571f",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/unicode-linebreak/0.1.5/download"],
        strip_prefix = "unicode-linebreak-0.1.5",
        build_file = Label("//oxc_cli/crates:BUILD.unicode-linebreak-0.1.5.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__unicode-segmentation-1.12.0",
        sha256 = "f6ccf251212114b54433ec949fd6a7841275f9ada20dddd2f29e9ceea4501493",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/unicode-segmentation/1.12.0/download"],
        strip_prefix = "unicode-segmentation-1.12.0",
        build_file = Label("//oxc_cli/crates:BUILD.unicode-segmentation-1.12.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__unicode-width-0.1.14",
        sha256 = "7dd6e30e90baa6f72411720665d41d89b9a3d039dc45b8faea1ddd07f617f6af",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/unicode-width/0.1.14/download"],
        strip_prefix = "unicode-width-0.1.14",
        build_file = Label("//oxc_cli/crates:BUILD.unicode-width-0.1.14.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__unicode-width-0.2.2",
        sha256 = "b4ac048d71ede7ee76d585517add45da530660ef4390e49b098733c6e897f254",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/unicode-width/0.2.2/download"],
        strip_prefix = "unicode-width-0.2.2",
        build_file = Label("//oxc_cli/crates:BUILD.unicode-width-0.2.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__utf8parse-0.2.2",
        sha256 = "06abde3611657adf66d383f00b093d7faecc7fa57071cce2578660c9f1010821",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/utf8parse/0.2.2/download"],
        strip_prefix = "utf8parse-0.2.2",
        build_file = Label("//oxc_cli/crates:BUILD.utf8parse-0.2.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__version_check-0.9.5",
        sha256 = "0b928f33d975fc6ad9f86c8f283853ad26bdd5b10b7f1542aa2fa15e2289105a",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/version_check/0.9.5/download"],
        strip_prefix = "version_check-0.9.5",
        build_file = Label("//oxc_cli/crates:BUILD.version_check-0.9.5.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__vsimd-0.8.0",
        sha256 = "5c3082ca00d5a5ef149bb8b555a72ae84c9c59f7250f013ac822ac2e49b19c64",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/vsimd/0.8.0/download"],
        strip_prefix = "vsimd-0.8.0",
        build_file = Label("//oxc_cli/crates:BUILD.vsimd-0.8.0.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows-link-0.2.1",
        sha256 = "f0805222e57f7521d6a62e36fa9163bc891acd422f971defe97d64e70d0a4fe5",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows-link/0.2.1/download"],
        strip_prefix = "windows-link-0.2.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows-link-0.2.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows-sys-0.60.2",
        sha256 = "f2f500e4d28234f72040990ec9d39e3a6b950f9f22d3dba18416c35882612bcb",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows-sys/0.60.2/download"],
        strip_prefix = "windows-sys-0.60.2",
        build_file = Label("//oxc_cli/crates:BUILD.windows-sys-0.60.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows-sys-0.61.2",
        sha256 = "ae137229bcbd6cdf0f7b80a31df61766145077ddf49416a728b02cb3921ff3fc",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows-sys/0.61.2/download"],
        strip_prefix = "windows-sys-0.61.2",
        build_file = Label("//oxc_cli/crates:BUILD.windows-sys-0.61.2.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows-targets-0.53.5",
        sha256 = "4945f9f551b88e0d65f3db0bc25c33b8acea4d9e41163edf90dcd0b19f9069f3",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows-targets/0.53.5/download"],
        strip_prefix = "windows-targets-0.53.5",
        build_file = Label("//oxc_cli/crates:BUILD.windows-targets-0.53.5.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows_aarch64_gnullvm-0.53.1",
        sha256 = "a9d8416fa8b42f5c947f8482c43e7d89e73a173cead56d044f6a56104a6d1b53",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows_aarch64_gnullvm/0.53.1/download"],
        strip_prefix = "windows_aarch64_gnullvm-0.53.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows_aarch64_gnullvm-0.53.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows_aarch64_msvc-0.53.1",
        sha256 = "b9d782e804c2f632e395708e99a94275910eb9100b2114651e04744e9b125006",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows_aarch64_msvc/0.53.1/download"],
        strip_prefix = "windows_aarch64_msvc-0.53.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows_aarch64_msvc-0.53.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows_i686_gnu-0.53.1",
        sha256 = "960e6da069d81e09becb0ca57a65220ddff016ff2d6af6a223cf372a506593a3",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows_i686_gnu/0.53.1/download"],
        strip_prefix = "windows_i686_gnu-0.53.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows_i686_gnu-0.53.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows_i686_gnullvm-0.53.1",
        sha256 = "fa7359d10048f68ab8b09fa71c3daccfb0e9b559aed648a8f95469c27057180c",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows_i686_gnullvm/0.53.1/download"],
        strip_prefix = "windows_i686_gnullvm-0.53.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows_i686_gnullvm-0.53.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows_i686_msvc-0.53.1",
        sha256 = "1e7ac75179f18232fe9c285163565a57ef8d3c89254a30685b57d83a38d326c2",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows_i686_msvc/0.53.1/download"],
        strip_prefix = "windows_i686_msvc-0.53.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows_i686_msvc-0.53.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows_x86_64_gnu-0.53.1",
        sha256 = "9c3842cdd74a865a8066ab39c8a7a473c0778a3f29370b5fd6b4b9aa7df4a499",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows_x86_64_gnu/0.53.1/download"],
        strip_prefix = "windows_x86_64_gnu-0.53.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows_x86_64_gnu-0.53.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows_x86_64_gnullvm-0.53.1",
        sha256 = "0ffa179e2d07eee8ad8f57493436566c7cc30ac536a3379fdf008f47f6bb7ae1",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows_x86_64_gnullvm/0.53.1/download"],
        strip_prefix = "windows_x86_64_gnullvm-0.53.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows_x86_64_gnullvm-0.53.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__windows_x86_64_msvc-0.53.1",
        sha256 = "d6bbff5f0aada427a1e5a6da5f1f98158182f26556f345ac9e04d36d0ebed650",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/windows_x86_64_msvc/0.53.1/download"],
        strip_prefix = "windows_x86_64_msvc-0.53.1",
        build_file = Label("//oxc_cli/crates:BUILD.windows_x86_64_msvc-0.53.1.bazel"),
    )

    maybe(
        http_archive,
        name = "rules_typescript_crates__zmij-1.0.21",
        sha256 = "b8848ee67ecc8aedbaf3e4122217aff892639231befc6a1b58d29fff4c2cabaa",
        type = "tar.gz",
        urls = ["https://static.crates.io/crates/zmij/1.0.21/download"],
        strip_prefix = "zmij-1.0.21",
        build_file = Label("//oxc_cli/crates:BUILD.zmij-1.0.21.bazel"),
    )

    return [
        struct(repo = "rules_typescript_crates__clap-4.5.60", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__miette-7.6.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__oxc_allocator-0.117.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__oxc_codegen-0.117.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__oxc_diagnostics-0.117.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__oxc_isolated_declarations-0.117.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__oxc_parser-0.117.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__oxc_semantic-0.117.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__oxc_span-0.117.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__oxc_transformer-0.117.0", is_dev_dep = False),
        struct(repo = "rules_typescript_crates__rayon-1.11.0", is_dev_dep = False),
    ]
