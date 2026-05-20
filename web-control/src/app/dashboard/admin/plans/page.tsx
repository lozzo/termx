import { getAdminPlans } from "@/lib/queries";
import AdminPlansClient from "@/components/admin/AdminPlansClient";

export default async function PlansAdminPage() {
  const plans = await getAdminPlans();
  return <AdminPlansClient initialPlans={JSON.parse(JSON.stringify(plans))} />;
}
