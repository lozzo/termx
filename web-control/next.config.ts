import type { NextConfig } from "next";
import { createMDX } from "fumadocs-mdx/next";

const nextConfig: NextConfig = {
  output: "standalone",
  async rewrites() {
    return [
      {
        source: "/remote",
        destination: "/remote/index.html",
      },
    ];
  },
};

const withMDX = createMDX();

export default withMDX(nextConfig);
