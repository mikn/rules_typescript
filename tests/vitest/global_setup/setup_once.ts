type Provide = (key: "rulesTsGlobalSetup", value: string) => void;

export default function setup({ provide }: { provide: Provide }): void {
  provide("rulesTsGlobalSetup", "ran");
}
