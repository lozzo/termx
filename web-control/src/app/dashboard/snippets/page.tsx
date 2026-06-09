import { getServerUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import { db } from "@/lib/db";
import { userSnippets } from "@/lib/schema";
import { getUserSubscription } from "@/lib/queries";
import { eq, desc } from "drizzle-orm";
import SnippetsClient from "@/components/SnippetsClient";

export default async function SnippetsPage() {
  const user = await getServerUser();
  if (!user) redirect("/login");

  const [snippets, subscription] = await Promise.all([
    db
      .select()
      .from(userSnippets)
      .where(eq(userSnippets.userId, user.id))
      .orderBy(desc(userSnippets.createdAt)),
    getUserSubscription(user.id),
  ]);

  return (
    <SnippetsClient
      initialSnippets={JSON.parse(JSON.stringify(snippets))}
      hasSubscription={!!subscription}
    />
  );
}
