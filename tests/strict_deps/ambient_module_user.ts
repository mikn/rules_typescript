import { authenticate, type AuthResult } from "acme-bridge";

export function ok(): boolean {
  const result: AuthResult = authenticate();
  return result.ok;
}
