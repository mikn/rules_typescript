import { nanoid } from "nanoid";

import { add, PI } from "./lib";

export const id: string = nanoid();
export const result: number = add(1, 2);
export const tau: number = PI * 2;
