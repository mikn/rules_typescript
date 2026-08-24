/**
 * Nothing here is declared by any `lib`: `process`, `setTimeout`, `Buffer` and
 * `NodeJS.Timeout` are globals only @types/node declares, and `node:fs` is a
 * module only its `declare module` blocks name.
 */

import { readFileSync } from 'node:fs';

export function readAfterTick(file: string): Promise<string> {
  const cwd: string = process.cwd();
  return new Promise<string>((resolve) => {
    const timer: NodeJS.Timeout = setTimeout(() => {
      const bytes: Buffer = readFileSync(file);
      resolve(`${cwd}:${bytes.byteLength}`);
    }, 0);
    timer.unref();
  });
}
