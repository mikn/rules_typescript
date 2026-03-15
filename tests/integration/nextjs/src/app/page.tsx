import { greet } from "../lib/greeting";

export default function Home() {
  return (
    <main>
      <h1>{greet("Bazel")}</h1>
    </main>
  );
}
