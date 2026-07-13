import type { Metadata } from "next";
import "./globals.css";
import "./controller.css";
import { Geist } from "next/font/google";

const geist = Geist({ subsets: ["latin"], variable: "--font-sans" });

export const metadata: Metadata = {
  title: "TermX - Your terminals, reachable anywhere",
  description:
    "Direct P2P when possible. Managed Relay when networks disagree. Terminal authorization stays end to end.",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" data-wx-theme="light-gray" className={geist.variable}>
      <body>{children}</body>
    </html>
  );
}
