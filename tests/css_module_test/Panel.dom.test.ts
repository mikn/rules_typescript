// The assertion this whole rule exists for: the class attribute a component
// renders is the scoped name a bundler emits, so a DOM test can be written
// against it. Under the old mock -- a Proxy returning the property name -- every
// expectation below held for the string "button", which no browser ever sees.
import { describe, it, expect } from "vitest";
import styles from "./Button.module.css";
import { renderPanel } from "./Panel";

const SCOPED = /^_button_[0-9a-f]{8}$/;

describe("a rendered class attribute", () => {
  it("is the name the export map holds", () => {
    const host = document.createElement("div");
    renderPanel(host);
    expect(host.querySelector("button")?.getAttribute("class")).toBe(styles.button);
  });

  it("is scoped, not the name written in the stylesheet", () => {
    expect(styles.button).toMatch(SCOPED);
    expect(styles.button).not.toBe("button");
  });

  it("is scoped by the stylesheet's content, so one file yields one hash", () => {
    const hashes = new Set(
      [styles.container, styles.button, styles.label].map((name) => name.split("_").pop()),
    );
    expect(hashes.size).toBe(1);
  });
});
