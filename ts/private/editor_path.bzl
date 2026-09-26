def editor_project_path(label):
    """Retains the emitted compiler config's package-local ambient lookup."""
    return "/".join([part for part in [label.package, ".bazel/tsconfig", label.name + ".json"] if part])
