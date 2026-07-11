import { getAdminUserOverrides } from "@/lib/queries";
import AdminOverridesClient from "@/components/admin/AdminOverridesClient";

export default async function OverridesAdminPage() {
  const overrides = await getAdminUserOverrides();
  return <AdminOverridesClient initialOverrides={JSON.parse(JSON.stringify(overrides))} />;
}
