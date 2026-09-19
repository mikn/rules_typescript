export const settled: string = await Promise.resolve("settled");
export let fallback: string | undefined;
fallback ||= settled;
