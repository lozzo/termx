import { getCurrentUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import { getActivePlans, getUserSubscription } from "@/lib/queries";
import BillingClient from "@/components/BillingClient";

export default async function BillingPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const [plans, subscription] = await Promise.all([
    getActivePlans(),
    getUserSubscription(user.userId),
  ]);

  return (
    <BillingClient
      initialPlans={JSON.parse(JSON.stringify(plans))}
      initialSubscription={JSON.parse(JSON.stringify(subscription))}
    />
  );
}
