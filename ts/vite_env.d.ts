/**
 * Ambient type shim for Vite client-side globals.
 *
 * Include this file in the srcs of any ts_compile target that uses Vite
 * client features such as import.meta.env, hot-module replacement, or
 * asset URL imports.
 *
 * Usage (manual):
 *   ts_compile(
 *       name = "app",
 *       srcs = ["src/main.tsx", "@rules_typescript//ts:vite_env.d.ts"],
 *   )
 *
 * Usage (via attr):
 *   ts_compile(
 *       name = "app",
 *       srcs = ["src/main.tsx"],
 *       vite_types = True,
 *   )
 *
 * This shim is intentionally standalone — it does not reference vite/client
 * so that the vite npm package does not need to be a compile-time dependency.
 */

// ── import.meta.env ──────────────────────────────────────────────────────────

interface ImportMetaEnv {
  /** Build mode: "production" in prod builds, "development" otherwise. */
  readonly MODE: string;
  /** Base URL as configured in vite.config (default: "/"). */
  readonly BASE_URL: string;
  /** True in production build, false in dev server. */
  readonly PROD: boolean;
  /** True in dev server, false in production build. */
  readonly DEV: boolean;
  /** True when running in SSR context. */
  readonly SSR: boolean;
  /** Any user-defined VITE_* env variable or env_vars entry. */
  readonly [key: string]: string | boolean | undefined;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
  /** Vite HMR API (only available in dev server mode). */
  readonly hot?: {
    readonly data: Record<string, unknown>;
    accept(): void;
    accept(cb: (mod: unknown) => void): void;
    accept(dep: string, cb: (mod: unknown) => void): void;
    accept(deps: string[], cb: (mods: unknown[]) => void): void;
    dispose(cb: (data: Record<string, unknown>) => void): void;
    decline(): void;
    invalidate(message?: string): void;
    on(event: string, cb: (...args: unknown[]) => void): void;
  };
}

// ── Asset URL imports ────────────────────────────────────────────────────────
// These module declarations allow TypeScript to type-check asset imports.
// The runtime value is a URL string (or data URL for inlined assets).

declare module "*.svg" {
  const src: string;
  export default src;
}

declare module "*.png" {
  const src: string;
  export default src;
}

declare module "*.jpg" {
  const src: string;
  export default src;
}

declare module "*.jpeg" {
  const src: string;
  export default src;
}

declare module "*.gif" {
  const src: string;
  export default src;
}

declare module "*.webp" {
  const src: string;
  export default src;
}

declare module "*.avif" {
  const src: string;
  export default src;
}

declare module "*.ico" {
  const src: string;
  export default src;
}

declare module "*.bmp" {
  const src: string;
  export default src;
}

// ── Font imports ─────────────────────────────────────────────────────────────

declare module "*.woff" {
  const src: string;
  export default src;
}

declare module "*.woff2" {
  const src: string;
  export default src;
}

declare module "*.eot" {
  const src: string;
  export default src;
}

declare module "*.ttf" {
  const src: string;
  export default src;
}

declare module "*.otf" {
  const src: string;
  export default src;
}

// ── Media imports ────────────────────────────────────────────────────────────

declare module "*.mp4" {
  const src: string;
  export default src;
}

declare module "*.webm" {
  const src: string;
  export default src;
}

declare module "*.ogg" {
  const src: string;
  export default src;
}

declare module "*.mp3" {
  const src: string;
  export default src;
}

declare module "*.wav" {
  const src: string;
  export default src;
}

declare module "*.flac" {
  const src: string;
  export default src;
}

declare module "*.aac" {
  const src: string;
  export default src;
}

// ── CSS imports ──────────────────────────────────────────────────────────────
// Plain CSS files imported without the ?inline query import their content
// as a side effect (for bundlers). With ?inline they return the CSS string.

declare module "*.css" {
  const css: string;
  export default css;
}

declare module "*.module.css" {
  const classes: Record<string, string>;
  export default classes;
}

declare module "*.module.scss" {
  const classes: Record<string, string>;
  export default classes;
}

declare module "*.module.sass" {
  const classes: Record<string, string>;
  export default classes;
}

declare module "*.module.less" {
  const classes: Record<string, string>;
  export default classes;
}
