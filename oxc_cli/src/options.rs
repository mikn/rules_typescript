use clap::Parser;
use std::path::PathBuf;

/// oxc-bazel — transform TypeScript/JSX source files via the oxc compiler stack.
///
/// Accepts a list of .ts/.tsx input paths and emits .js, optional .js.map, and
/// optional .d.ts files to the specified output directory.  Designed to be
/// invoked as a Bazel action; exits immediately after processing all inputs.
#[derive(Debug, Parser)]
#[command(name = "oxc-bazel", version, about)]
pub struct CliOptions {
    /// Input .ts / .tsx source files to transform.
    #[arg(long, required = true, num_args = 1.., value_name = "FILE")]
    pub files: Vec<PathBuf>,

    /// Directory where all output files are written.
    #[arg(long, value_name = "DIR")]
    pub out_dir: PathBuf,

    /// ECMAScript target for the output.
    ///
    /// Accepted values: es2015, es2016, es2017, es2018, es2019, es2020,
    /// es2021, es2022, es2023, es2024, esnext.
    #[arg(long, default_value = "es2022", value_name = "TARGET")]
    pub target: String,

    /// JSX transform mode.
    ///
    /// Accepted values: react-jsx (automatic runtime), react (classic
    /// runtime), preserve (leave JSX as-is).
    #[arg(long, default_value = "react-jsx", value_name = "MODE")]
    pub jsx: String,

    /// JSX runtime variant.  "automatic" uses the new JSX transform
    /// (React 17+); "classic" uses React.createElement.
    #[arg(long, default_value = "automatic", value_name = "RUNTIME")]
    pub jsx_runtime: String,

    /// Package that provides the JSX runtime import (e.g. "react").
    #[arg(long, default_value = "react", value_name = "SOURCE")]
    pub jsx_import_source: Option<String>,

    /// Emit .d.ts declaration files using isolated-declarations semantics.
    /// Implies --declaration.
    #[arg(long)]
    pub isolated_declarations: bool,

    /// Emit .d.ts declaration files.  Automatically enabled when
    /// --isolated-declarations is set.
    #[arg(long)]
    pub declaration: bool,

    /// Emit .js.map source map files alongside every .js output.
    #[arg(long)]
    pub source_map: bool,

    /// Strip this prefix from every input path when computing the relative
    /// output path inside --out-dir.
    ///
    /// Example: with `--strip-dir-prefix src/`, the input `src/app/page.tsx`
    /// maps to `<out-dir>/app/page.js`.
    #[arg(long, value_name = "PREFIX")]
    pub strip_dir_prefix: Option<PathBuf>,
}

impl CliOptions {
    /// Whether .d.ts output is requested (either explicitly or implied).
    pub fn emit_declarations(&self) -> bool {
        self.declaration || self.isolated_declarations
    }
}
