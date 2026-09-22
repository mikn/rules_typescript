import { type Counter, initial } from "./state";

export const engineUrl: string = import.meta.url;

export function step(counter: Counter = initial): Counter {
  return { count: counter.count + 1 };
}
