declare const process: {
  readonly env: Record<string, string | undefined>;
  readonly argv: string[];
  cwd(): string;
};
