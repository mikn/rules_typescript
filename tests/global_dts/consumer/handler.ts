export function currentToken(): string {
  return CLOUDFLARE_BINDINGS.API_TOKEN;
}

export function handler(env: Env): string {
  return env.API_TOKEN;
}
