import { getCurrentUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import { getDashboardStats, getUserSubscription } from "@/lib/queries";
import DashboardOverview from "@/components/DashboardOverview";

export default async function DashboardPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const [stats, subscription] = await Promise.all([
    getDashboardStats(user.userId),
    getUserSubscription(user.userId),
  ]);

  return (
    <DashboardOverview
      user={{ username: user.username }}
      stats={stats}
      subscription={JSON.parse(JSON.stringify(subscription))}
    />
  );
}
