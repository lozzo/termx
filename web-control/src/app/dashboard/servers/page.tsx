import { getCurrentUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import { getUserAgents, getUserSubscription } from "@/lib/queries";
import ServersClient from "@/components/ServersClient";

export default async function ServersPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const [agents, subscription] = await Promise.all([
    getUserAgents(user.userId),
    getUserSubscription(user.userId),
  ]);

  return (
    <ServersClient
      initialAgents={JSON.parse(JSON.stringify(agents))}
      hasSubscription={!!subscription}
    />
  );
}
