import { getAdminUsers } from "@/lib/queries";
import AdminUsersClient from "@/components/admin/AdminUsersClient";

export default async function UsersAdminPage() {
  const users = await getAdminUsers();
  return <AdminUsersClient initialUsers={JSON.parse(JSON.stringify(users))} />;
}
