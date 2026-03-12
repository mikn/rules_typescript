// Header component that imports an SVG asset.
// The import returns a URL string at runtime (handled by the bundler).
import logo from "./logo.svg";

export interface HeaderProps {
  title: string;
}

/**
 * Returns metadata about the header including the logo URL.
 * Demonstrates that TypeScript accepts SVG asset imports as string URLs.
 */
export function getHeaderInfo(props: HeaderProps): { title: string; logoUrl: string } {
  return {
    title: props.title,
    logoUrl: logo,
  };
}

// Explicitly typed for isolated declarations compatibility.
export const LOGO_URL: string = logo as string;
