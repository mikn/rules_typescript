import styles from "./button.module.css";
import logoUrl from "./logo.svg";
import config from "./config.json";

export interface ButtonProps {
  label?: string;
  variant?: "primary" | "secondary";
}

export function Button({ label = config.buttonLabel, variant = "primary" }: ButtonProps): JSX.Element {
  const variantClass = variant === "primary" ? styles.buttonPrimary : styles.buttonSecondary;
  return (
    <div className={styles.container}>
      <img src={logoUrl} alt="logo" width={24} />
      <button className={`${styles.button} ${variantClass}`}>{label}</button>
    </div>
  );
}
