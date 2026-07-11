import { getAdminAgentReleases, getAdminAppReleases } from "@/lib/queries";
import AdminReleasesClient from "@/components/admin/AdminReleasesClient";

export default async function ReleasesAdminPage() {
  const [agentReleases, appReleases] = await Promise.all([
    getAdminAgentReleases(),
    getAdminAppReleases(),
  ]);
  return (
    <AdminReleasesClient
      initialAgentReleases={JSON.parse(JSON.stringify(agentReleases))}
      initialAppReleases={JSON.parse(JSON.stringify(appReleases))}
    />
  );
}
