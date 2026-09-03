import {greet} from "#shared/util";

export function consume(): string {
	return greet("consumer");
}
