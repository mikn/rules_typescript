import type { ReactElement, ReactNode } from "react";

export function Banner({ children }: { children?: ReactNode }): ReactElement {
  return <div className="banner">{children}</div>;
}
