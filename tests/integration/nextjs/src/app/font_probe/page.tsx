import { Inter } from "next/font/google";

const inter = Inter({ subsets: ["latin"] });

export default function FontProbe() {
  return <p className={inter.className}>FONT_PROBE</p>;
}
