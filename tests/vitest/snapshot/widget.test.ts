import { expect, test } from "vitest";

import { renderWidget } from "./widget";

test("widget markup", () => {
  expect(renderWidget("ok")).toMatchSnapshot();
});
