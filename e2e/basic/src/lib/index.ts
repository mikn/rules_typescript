/**
 * Public API surface for the lib package.
 *
 * Downstream targets depend on //src/lib and import from this barrel file.
 * Re-exporting with explicit names avoids star-export ambiguity, which is
 * required when isolatedDeclarations is enabled.
 */

export { add, divide, multiply, subtract } from "./math";
