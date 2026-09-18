import { greet } from "./js_consumer.js";

export function shoutedGreeting(count: number): string {
    return greet(count).toUpperCase();
}
