// This runs inside workerd, not Node: the pool boots the worker from
// wrangler.jsonc and SELF dispatches to its fetch handler. A Node-pool run
// would have no SELF to import, which is what makes this a real pool test
// rather than a unit test that happens to import the module.
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
