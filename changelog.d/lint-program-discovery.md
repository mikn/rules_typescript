### Fixed

Type aware linters that discover the nearest tsconfig now use the generated target program, including emitted sibling declarations. The compiler's existing configuration pass preserves inherited options in the canonical program without a circular extends chain.
