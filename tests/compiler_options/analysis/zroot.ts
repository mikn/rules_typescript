import { leaf } from "./nested/leaf.js";
import { helper } from "./util.js";

export const value = leaf() + helper(1);
