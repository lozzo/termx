import { getServerUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import { db } from "@/lib/db";
import { userPathBookmarks, agents } from "@/lib/schema";
import { getUserSubscription } from "@/lib/queries";
import { eq, desc } from "drizzle-orm";
import PathBookmarksClient from "@/components/PathBookmarksClient";

export default async function PathBookmarksPage() {
  const user = await getServerUser();
  if (!user) redirect("/login");

  const [bookmarks, userAgents, subscription] = await Promise.all([
    db
      .select()
      .from(userPathBookmarks)
      .where(eq(userPathBookmarks.userId, user.id))
      .orderBy(desc(userPathBookmarks.createdAt)),
    db
      .select({ id: agents.id, name: agents.name, hostname: agents.hostname })
      .from(agents)
      .where(eq(agents.userId, user.id)),
    getUserSubscription(user.id),
  ]);

  return (
    <PathBookmarksClient
      initialBookmarks={JSON.parse(JSON.stringify(bookmarks))}
      agents={JSON.parse(JSON.stringify(userAgents))}
      hasSubscription={!!subscription}
    />
  );
}
