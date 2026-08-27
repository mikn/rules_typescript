"use server";

export async function echoAction(value: string): Promise<string> {
  return `echoed:${value}`;
}
