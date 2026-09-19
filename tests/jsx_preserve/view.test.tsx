import { expect, test } from "vitest";

import { banner } from "./uses_view";
import { view } from "./view";

test("the view is the element the runtime builds", () => {
  expect(view("hi")).toEqual({
    type: "p",
    props: { id: "root", children: "hi" },
  });
});

test("a consumer of the .tsx reaches it through its compiled dep", () => {
  expect(banner()).toEqual(view("banner"));
});

test("JSX in the test file itself compiles the same way", () => {
  expect(<p id="root">hi</p>).toEqual(view("hi"));
});
