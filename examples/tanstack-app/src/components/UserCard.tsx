/**
 * UserCard component — displays user information.
 *
 * Demonstrates a data-display component with explicit prop types.
 */

import type { ReactElement } from "react";

export interface UserCardProps {
  id: string;
  name: string;
  email: string;
  role: "admin" | "user" | "viewer";
}

export function UserCard(props: UserCardProps): ReactElement {
  const { name, email, role } = props;

  return (
    <div className={`user-card user-card--${role}`}>
      <h3 className="user-card__name">{name}</h3>
      <p className="user-card__email">{email}</p>
      <span className="user-card__role">{role}</span>
    </div>
  );
}
