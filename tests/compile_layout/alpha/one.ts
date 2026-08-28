import { three } from "./three.js";
import { two } from "../beta/deep/two.js";

export const one = (): number => two() + three();
