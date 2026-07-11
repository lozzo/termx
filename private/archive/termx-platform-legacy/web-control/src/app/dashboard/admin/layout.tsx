import { getCurrentUser } from "@/lib/auth";
import { notFound } from "next/navigation";

export default async function AdminLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const user = await getCurrentUser();

  if (!user || user.role !== "admin") {
    notFound();
  }

  return <>{children}</>;
}
