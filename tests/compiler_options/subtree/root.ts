import { deepLeaf } from "./nested/deep/leaf.js";
import { double } from "./nested/helper.js";

export const total = double(2) + deepLeaf();
