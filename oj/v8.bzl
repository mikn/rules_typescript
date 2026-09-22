"""Declared upstream V8 libraries used by oj's source build."""

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_file")

_ARCHIVES = {
    "linux_amd64": ("librusty_v8_simdutf_release_x86_64-unknown-linux-gnu.a.gz", "f48762ca10d1f1fc605a441c5ae430ec8ce1e9e80f14d78fbc42cb878c30b476"),
    "linux_arm64": ("librusty_v8_simdutf_release_aarch64-unknown-linux-gnu.a.gz", "539e283815a396a5796f32858b42e517b858ebaaeaaad05d03290ee8c864a527"),
    "darwin_amd64": ("librusty_v8_simdutf_release_x86_64-apple-darwin.a.gz", "a750271fec6b211457ed0a5cf7d2eab1924b265621a82da86ab959d6ff0823e4"),
    "darwin_arm64": ("librusty_v8_simdutf_release_aarch64-apple-darwin.a.gz", "5aeffd8d5a0c1b79ac1d70af83d5b19099655fd9c645a794dc43f101f779838c"),
    "windows_amd64": ("rusty_v8_simdutf_release_x86_64-pc-windows-msvc.lib.gz", "f231f82cbacb9aefe6d9af57e6df2e8959a40e001f79306485133e3c075b98f0"),
}

def _v8_archives_impl(_ctx):
    for platform, (filename, sha256) in _ARCHIVES.items():
        http_file(
            name = "oj_v8_" + platform,
            urls = ["https://github.com/denoland/rusty_v8/releases/download/v150.4.0/" + filename],
            sha256 = sha256,
            downloaded_file_path = filename,
        )

v8_archives = module_extension(implementation = _v8_archives_impl)
