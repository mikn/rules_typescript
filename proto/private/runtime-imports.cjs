const { writeFileSync } = require("node:fs");
const { wktPublicImportPaths } = require("@bufbuild/protobuf/codegenv2");

writeFileSync(process.argv[2], JSON.stringify(wktPublicImportPaths));
