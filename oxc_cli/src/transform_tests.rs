use clap::Parser;

use super::build_transform_options;
use crate::options::CliOptions;

#[test]
fn top_level_await_flag_preserves_other_target_transforms() {
    for (extra, reject_top_level_await) in
        [(&[][..], true), (&["--top-level-await"][..], false)]
    {
        let opts = CliOptions::parse_from(
            [
                "oxc-bazel",
                "--files",
                "input.ts",
                "--out-dir",
                "out",
                "--target",
                "es2020",
            ]
            .into_iter()
            .chain(extra.iter().copied()),
        );
        let options = build_transform_options(&opts).unwrap();

        assert_eq!(options.env.es2022.top_level_await, reject_top_level_await);
        assert!(options.env.es2021.logical_assignment_operators);
    }
}
