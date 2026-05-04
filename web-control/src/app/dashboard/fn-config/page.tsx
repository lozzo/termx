import { getServerUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import { db } from "@/lib/db";
import { userFnConfigs } from "@/lib/schema";
import { getUserSubscription } from "@/lib/queries";
import { eq } from "drizzle-orm";
import FnConfigClient from "@/components/FnConfigClient";

export default async function FnConfigPage() {
  const user = await getServerUser();
  if (!user) redirect("/login");

  const [row, subscription] = await Promise.all([
    db.query.userFnConfigs.findFirst({
      where: eq(userFnConfigs.userId, user.id),
    }),
    getUserSubscription(user.id),
  ]);

  const config = row ? JSON.parse(row.config) : null;

  return <FnConfigClient initialConfig={config} hasSubscription={!!subscription} />;
}
