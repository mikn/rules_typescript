import Image from "next/image";

import { fixtureVersion } from "../../shared/version";
import { greet } from "../lib/greeting";
import logo from "../../public/logo.png";

export default function Home() {
  return (
    <main>
      <h1>{greet("Bazel")}</h1>
      <p>{fixtureVersion}</p>
      <Image src={logo} alt="logo" />
    </main>
  );
}
