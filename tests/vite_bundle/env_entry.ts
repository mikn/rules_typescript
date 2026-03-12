// env_entry.ts — exercises import.meta.env substitution.
// ts_compile uses vite_types = True so these usages type-check.
// ts_bundle uses env_vars = {"VITE_API_URL": "https://api.example.com"} so
// the bundler replaces import.meta.env.VITE_API_URL with the literal string.

export function getApiUrl(): string {
  return import.meta.env.VITE_API_URL as string;
}

export const isProd: boolean = import.meta.env.PROD;
