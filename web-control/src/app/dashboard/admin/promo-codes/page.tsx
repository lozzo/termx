import { getAdminPromoCodes } from "@/lib/queries";
import AdminPromoCodesClient from "@/components/admin/AdminPromoCodesClient";

export default async function PromoCodesAdminPage() {
  const codes = await getAdminPromoCodes();
  return <AdminPromoCodesClient initialCodes={JSON.parse(JSON.stringify(codes))} />;
}
