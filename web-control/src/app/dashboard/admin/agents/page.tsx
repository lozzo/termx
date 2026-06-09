import { getAdminAgents } from "@/lib/queries";
import AdminAgentsClient from "@/components/admin/AdminAgentsClient";

export default async function AgentsAdminPage() {
  const agents = await getAdminAgents();
  return <AdminAgentsClient initialAgents={JSON.parse(JSON.stringify(agents))} />;
}
