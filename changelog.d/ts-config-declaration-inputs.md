### Changed

- **Configuration resolves generated ambient files from their declared paths.**
  Individual generated dependency files no longer delay configuration until
  their contents exist. Source files and generated directories remain inputs.
  Programs whose ES module kind is validated during configuration emit from
  sources and resolved options; CommonJS programs retain their full input
  closure. Declaration emission and type validation still
  read the dependency contents.
