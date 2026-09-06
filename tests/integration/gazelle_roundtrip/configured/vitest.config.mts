// The one setting test/ needs, a plugin answering `virtual:answer`, kept where
// plain `vitest` reads it: beside package.json, one package above the test.
type Plugin = {
	name: string;
	resolveId(id: string): string | null;
	load(id: string): string | null;
};

const answerPlugin: Plugin = {
	name: "roundtrip-package-root-answer",
	resolveId(id: string): string | null {
		return id === "virtual:answer" ? "\0virtual:answer" : null;
	},
	load(id: string): string | null {
		return id === "\0virtual:answer" ? "export default 42;" : null;
	},
};

const config: {plugins: Plugin[]} = {plugins: [answerPlugin]};

export default config;
