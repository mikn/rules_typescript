import { describe, expect, it } from "vitest";

describe("happy-dom environment", () => {
  it("gives the test a document to mutate", () => {
    const host = document.createElement("div");
    host.innerHTML = '<button type="button">Save</button>';
    document.body.append(host);

    const button = document.querySelector("button");
    expect(button?.textContent).toBe("Save");
    expect(document.body.contains(button)).toBe(true);
  });

  it("resolves the DOM globals vitest only installs for this environment", () => {
    expect(typeof window).toBe("object");
    expect(new DOMParser().parseFromString("<p>hi</p>", "text/html").body.textContent).toBe("hi");
  });
});
