#[test]
fn file_source_survives_compiler_sandbox() {
    let source = deno_core::__extension_include_js_files_inner!(
        @item mode=loaded,
        dir=env!("CARGO_MANIFEST_DIR"),
        specifier="ext:test/snapshot_fixture.js",
        file="snapshot_fixture.js"
    );
    assert!(!source.is_runtime_loadable());
    assert_eq!(
        source.load().unwrap().as_str(),
        include_str!("snapshot_fixture.js")
    );
}

#[test]
fn inline_source_keeps_runtime_registration() {
    let source = deno_core::__extension_include_js_files_inner!(
        @item mode=loaded,
        specifier="ext:test/inline.js",
        source="export const value = 42;"
    );
    assert!(source.is_runtime_loadable());
    assert_eq!(source.load().unwrap().as_str(), "export const value = 42;");
}
