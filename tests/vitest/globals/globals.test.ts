describe("globals", () => {
  it("has describe/it/expect with no import and no hand-written shim", () => {
    expect(1 + 1).toBe(2);
  });

  afterEach(() => {
    expect(true).toBe(true);
  });
});
