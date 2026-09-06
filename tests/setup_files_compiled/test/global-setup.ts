type Provide = (key: "setupFilesCompiled", value: string) => void;

export default function setup({ provide }: { provide: Provide }): void {
  provide("setupFilesCompiled", "ran");
}
