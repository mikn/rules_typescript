import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Next.js Bazel Integration Test",
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
