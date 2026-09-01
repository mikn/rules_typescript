import { farewell } from "./compiled/messages/farewell.js";

export function bye(count: number): string {
    return farewell(count);
}
