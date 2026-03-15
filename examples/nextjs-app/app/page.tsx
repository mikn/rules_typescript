import { greet } from "../packages/shared/src/index";

export default function Home() {
  const message = greet("Bazel");

  return (
    <main>
      <h1>{message}</h1>
      <p>
        This Next.js app is built with <code>rules_typescript</code> and the{" "}
        <code>next_build</code> Bazel rule.
      </p>
      <nav>
        <a href="/about">About</a>
      </nav>
    </main>
  );
}
