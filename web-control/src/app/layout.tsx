import type { Metadata } from "next";
import { AuthProvider } from "@/components/AuthProvider";
import { RootProvider } from "fumadocs-ui/provider";
import "./globals.css";

export const metadata: Metadata = {
  title: "termx - 下一代跨平台 TermX 远程终端",
  description: "P2P 实时直连，零代码赋予任何 CLI 工具与 AI Agent 现代化的原生移动端操控体验。",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN" className="dark" suppressHydrationWarning>
      <body>
        <AuthProvider>
          <RootProvider theme={{ defaultTheme: "dark", enabled: false }}>
            {children}
          </RootProvider>
        </AuthProvider>
      </body>
    </html>
  );
}
