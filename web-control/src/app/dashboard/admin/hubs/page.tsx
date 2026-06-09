import { getAdminHubs } from "@/lib/queries";
import AdminHubsClient from "@/components/admin/AdminHubsClient";

export default async function HubsAdminPage() {
  const hubs = await getAdminHubs();
  return <AdminHubsClient initialHubs={JSON.parse(JSON.stringify(hubs))} />;
}
