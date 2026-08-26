import logo from "./big_logo.svg";
import noise from "./noise.png";
import styles from "./panel.module.css";

export const logoUrl: string = logo;
export const noiseUrl: string = noise;
export const panelClass: string = styles.panel;

// Vite builds an app entry with preserveEntrySignatures: false, so an export is
// not a use: without a side effect reaching them, the asset and the stylesheet
// are tree-shaken out and there is nothing to assert about.
if (logoUrl === panelClass || noiseUrl === panelClass) {
  throw new Error("unreachable");
}
