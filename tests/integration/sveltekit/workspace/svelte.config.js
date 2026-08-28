// kit.version.name defaults to Date.now(), and it is hashed into every client
// chunk name: pinned here so two builds of the same sources agree.
export default {
  kit: {
    version: { name: "integration-test" },
  },
};
