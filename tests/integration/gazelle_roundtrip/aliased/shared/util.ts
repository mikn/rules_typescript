export function greet(name: string): string {
	return "hello " + name;
}

export type Greeting = ReturnType<typeof greet>;
