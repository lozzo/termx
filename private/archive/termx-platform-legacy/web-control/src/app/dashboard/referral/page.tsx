import { getServerUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import { getUserReferralStats } from "@/lib/queries";
import ReferralClient from "@/components/ReferralClient";

export default async function ReferralPage() {
  const user = await getServerUser();
  if (!user) redirect("/login");

  const referralStats = await getUserReferralStats(user.id);

  return (
    <ReferralClient
      initialReferralStats={referralStats}
    />
  );
}
