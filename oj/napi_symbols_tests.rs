#[test]
fn symbol_lists_follow_the_target_operating_system() {
    let linux = std::str::from_utf8(oj_deno_napi::symbol_exports("linux")).unwrap();
    let macos = std::str::from_utf8(oj_deno_napi::symbol_exports("macos")).unwrap();
    let windows = std::str::from_utf8(oj_deno_napi::symbol_exports("windows")).unwrap();
    assert!(linux.starts_with('{') && linux.contains("\"napi_get_version\";"));
    assert!(macos.lines().any(|line| line == "_napi_get_version"));
    assert!(
        windows.contains("EXPORTS")
            && windows
                .lines()
                .any(|line| line.trim() == "napi_get_version")
    );
    for target in ["freebsd", "openbsd", "android"] {
        assert_eq!(oj_deno_napi::symbol_exports(target), linux.as_bytes());
    }
}
