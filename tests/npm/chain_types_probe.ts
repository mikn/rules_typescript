/// <reference types="bun-types" />
import { createInterface } from "node:readline";
export const probe = createInterface({ input: process.stdin });
probe.on("line", (line: string) => void line);
