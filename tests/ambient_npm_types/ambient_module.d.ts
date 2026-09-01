declare module "acme-bridge" {
  export type AuthResult = { ok: boolean };
  export function authenticate(): AuthResult;
}
