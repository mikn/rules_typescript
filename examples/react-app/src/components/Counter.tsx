/**
 * Counter component that uses the useCounter hook.
 *
 * Demonstrates cross-package dependencies: this component imports from
 * //src/hooks via a relative import, which Gazelle resolves to the correct
 * Bazel label.
 */

import type { MouseEvent, ReactElement } from "react";
import { useCounter } from "../hooks";
import { Button } from "./Button";

export interface CounterProps {
  initialValue?: number;
  step?: number;
  label?: string;
}

export function Counter(props: CounterProps): ReactElement {
  const { initialValue = 0, step = 1, label = "Count" } = props;
  const { count, increment, decrement, reset } = useCounter(initialValue, step);

  const handleIncrement = (_event: MouseEvent<HTMLButtonElement>): void => {
    increment();
  };

  const handleDecrement = (_event: MouseEvent<HTMLButtonElement>): void => {
    decrement();
  };

  const handleReset = (_event: MouseEvent<HTMLButtonElement>): void => {
    reset();
  };

  return (
    <div className="counter">
      <span className="counter-label">{label}</span>
      <span className="counter-value">{count}</span>
      <div className="counter-controls">
        <Button label="-" onClick={handleDecrement} />
        <Button label="Reset" onClick={handleReset} variant="secondary" />
        <Button label="+" onClick={handleIncrement} />
      </div>
    </div>
  );
}
