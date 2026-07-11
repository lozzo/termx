import { docs } from "@/../.source";
import { loader } from "fumadocs-core/source";
import { createMDXSource } from "fumadocs-mdx";

const mdxSource = createMDXSource(docs.docs, docs.meta);

export const source = loader({
  baseUrl: "/docs",
  source: {
    files: typeof mdxSource.files === "function"
      ? (mdxSource.files as unknown as () => typeof mdxSource.files)()
      : mdxSource.files,
  },
});
