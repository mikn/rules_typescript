import { expect, test } from "vitest";

import { view } from "preserve-view";

test("the member's name reaches the .jsx its manifest as built names", () => {
  expect(view("hi")).toEqual({
    type: "p",
    props: { id: "root", children: "hi" },
  });
});
