/// <reference types="@cloudflare/vitest-pool-workers/types" />
// Runs inside workerd: a Node-pool run would have no SELF to import.
import { SELF } from 'cloudflare:test';
import { describe, expect, it } from 'vitest';

describe('worker', () => {
  it('answers /health', async () => {
    const res = await SELF.fetch('https://example.com/health');
    expect(res.status).toBe(200);
    expect(await res.text()).toBe('ok');
  });

  it('404s anything else', async () => {
    const res = await SELF.fetch('https://example.com/nope');
    expect(res.status).toBe(404);
  });
});
