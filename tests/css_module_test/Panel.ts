import styles from "./Button.module.css";

export function renderPanel(host: HTMLElement): void {
  const button = host.ownerDocument.createElement("button");
  button.className = styles.button;
  host.replaceChildren(button);
}
