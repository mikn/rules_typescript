import { readFileSync } from "node:fs";
import data from "./lib_data.json";

export const answer: number = data.answer;

export const note = (): string =>
  readFileSync(new URL("./lib_note.txt", import.meta.url), "utf8").trim();
