import { getAdminOrders } from "@/lib/queries";
import AdminOrdersClient from "@/components/admin/AdminOrdersClient";

export default async function OrdersAdminPage() {
  const { stats, orders } = await getAdminOrders();
  return (
    <AdminOrdersClient
      initialOrders={JSON.parse(JSON.stringify(orders))}
      initialStats={JSON.parse(JSON.stringify(stats))}
    />
  );
}
