import { getServerUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import { getUserTokens, getUserSubscription } from "@/lib/queries";
import TokensClient from "@/components/TokensClient";

export default async function TokensPage() {
  const user = await getServerUser();
  if (!user) redirect("/login");

  const [tokens, subscription] = await Promise.all([
    getUserTokens(user.id),
    getUserSubscription(user.id),
  ]);

  return (
    <TokensClient
      initialTokens={JSON.parse(JSON.stringify(tokens))}
      hasSubscription={!!subscription}
    />
  );
}
