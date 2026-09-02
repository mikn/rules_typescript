import { apiUrl } from "../vite_lib/index";

export function endpoint(): string {
  return `${apiUrl()}?mode=${import.meta.env.MODE}`;
}
