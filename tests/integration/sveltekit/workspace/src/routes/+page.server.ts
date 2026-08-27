import { greeting } from "$lib/greeting";

export const load = (): { token: string; hello: string } => ({
  token: "acme-server-marker",
  hello: greeting(),
});
