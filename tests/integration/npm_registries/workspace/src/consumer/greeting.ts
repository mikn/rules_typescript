import { greet } from "@acme/greeter";
import { greet as greetLumen } from "@lumen/greeter";

export const message: string = greet("the .npmrc's registry");
export const lumenMessage: string = greetLumen("the workspace file's registry");
