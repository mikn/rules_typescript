describe("attribute layer", () => {
  it("exposes describe/it/expect without an import", () => {
    expect(globalThis.__rulesTsAttrsSetup).toBe("ran");
  });
});
