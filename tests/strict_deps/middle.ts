import { leaf, type Leaf } from "./leaf";

export interface Middle {
  id: string;
  base: Leaf;
}

export const middle: Middle = { id: "middle", base: leaf };
