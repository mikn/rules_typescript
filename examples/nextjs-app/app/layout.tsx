import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Next.js on Bazel",
  description: "Demonstrates rules_typescript next_build rule",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
