import { getAdminSubscriptions } from "@/lib/queries";
import AdminSubscriptionsClient from "@/components/admin/AdminSubscriptionsClient";

export default async function SubscriptionsAdminPage() {
  const subs = await getAdminSubscriptions();
  return <AdminSubscriptionsClient initialSubs={JSON.parse(JSON.stringify(subs))} />;
}
