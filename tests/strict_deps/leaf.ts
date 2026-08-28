import { hidden, type Hidden } from "./hidden";

export interface Leaf {
  id: string;
  base: Hidden;
}

export const leaf: Leaf = { id: "leaf", base: hidden };

export default leaf;
