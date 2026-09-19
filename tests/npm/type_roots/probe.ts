/// <reference types="bun-types" />
import { createInterface } from "node:readline";
export const lines = createInterface({ input: process.stdin });
lines.on("line", (line: string) => void line);
