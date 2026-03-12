/**
 * useCounter — a simple counter hook demonstrating custom hook patterns.
 *
 * Return type is explicit for isolated declarations compatibility.
 */

import { useState } from "react";

export interface UseCounterResult {
  count: number;
  increment: () => void;
  decrement: () => void;
  reset: () => void;
  setValue: (value: number) => void;
}

export function useCounter(
  initialValue: number = 0,
  step: number = 1,
): UseCounterResult {
  const [count, setCount] = useState<number>(initialValue);

  const increment = (): void => {
    setCount((prev) => prev + step);
  };

  const decrement = (): void => {
    setCount((prev) => prev - step);
  };

  const reset = (): void => {
    setCount(initialValue);
  };

  const setValue = (value: number): void => {
    setCount(value);
  };

  return { count, increment, decrement, reset, setValue };
}
