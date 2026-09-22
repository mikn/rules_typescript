#[test]
fn assembler_definitions_follow_the_consumer_output_directory() {
    let output = std::path::PathBuf::from(std::env::var_os("TEST_TMPDIR").unwrap());
    let compilations =
        cranelift_codegen_meta::isle::get_isle_compilations(std::path::Path::new("."), &output);
    let definitions = output.join("assembler-definitions.isle");
    assert_eq!(
        std::fs::read_to_string(&definitions).unwrap(),
        cranelift_assembler_x64::isle_definitions(),
    );
    assert!(
        cranelift_assembler_x64::generated_files()
            .iter()
            .all(|path| path.is_relative())
    );
    let mut found = false;
    for compilation in compilations.items {
        for input in compilation.inputs {
            if input.file_name() == Some(std::ffi::OsStr::new("assembler-definitions.isle")) {
                assert_eq!(input, definitions);
                found = true;
            }
        }
    }
    assert!(found);
}
