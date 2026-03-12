mod options;
mod transform;

use std::process;
use std::sync::{Arc, Mutex};

use clap::Parser as ClapParser;
use miette::Report;
use rayon::prelude::*;

use crate::options::CliOptions;
use crate::transform::transform_file;

fn main() {
    // Install miette's fancy error hook for rich terminal output.
    miette::set_hook(Box::new(|_| {
        Box::new(
            miette::MietteHandlerOpts::new()
                .terminal_links(true)
                .unicode(true)
                .context_lines(3)
                .build(),
        )
    }))
    .expect("Failed to install miette error hook");

    let opts = Arc::new(CliOptions::parse());

    if opts.files.is_empty() {
        eprintln!("error: no input files specified");
        process::exit(1);
    }

    // Collect errors from all parallel workers.
    let errors: Arc<Mutex<Vec<Report>>> = Arc::new(Mutex::new(Vec::new()));

    opts.files.par_iter().for_each(|input_path| {
        if let Err(report) = transform_file(input_path, &opts) {
            errors
                .lock()
                .expect("error mutex poisoned")
                .push(report);
        }
    });

    let errors = Arc::try_unwrap(errors)
        .expect("Arc still has multiple owners after rayon join")
        .into_inner()
        .expect("error mutex poisoned");

    if !errors.is_empty() {
        for report in &errors {
            eprintln!("{report:?}");
        }
        process::exit(1);
    }
}
