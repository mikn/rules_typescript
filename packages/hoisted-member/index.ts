import { z } from "zod";

const Name = z.string().min(1);

export function greet(name: string): string {
    return `hello, ${Name.parse(name)}`;
}
