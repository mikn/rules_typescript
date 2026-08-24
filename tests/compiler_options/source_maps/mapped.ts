export interface Mapped {
  readonly label: string;
}

export const makeMapped = (label: string): Mapped => ({ label });
