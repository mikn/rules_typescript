describe("the user's config", () => {
  it("installs describe/it/expect as globals", () => {
    expect(typeof describe).toBe("function");
  });
});
