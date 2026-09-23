### Added

- `ts_proto_library` generates a TypeScript source library from one `proto_library` through protobuf protoc and pinned protobuf-es. The Bazel graph owns schema selection and imports; generated files use the existing source-mode compiler and validation path.
