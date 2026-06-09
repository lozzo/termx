import { getServerUser } from "@/lib/auth";
import { redirect } from "next/navigation";
import SettingsClient from "@/components/SettingsClient";

interface SettingsPageProps {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}

function getSafeReturnPath(value?: string | string[]): string | null {
  const candidate = Array.isArray(value) ? value[0] : value;
  if (!candidate) return null;
  return candidate.startsWith("/") && !candidate.startsWith("//") ? candidate : null;
}

export default async function SettingsPage({ searchParams }: SettingsPageProps) {
  const user = await getServerUser();
  if (!user) redirect("/login");
  const resolvedSearchParams = searchParams ? await searchParams : {};
  const setupLocalPassword = resolvedSearchParams.setupLocalPassword === "1";
  const from = getSafeReturnPath(resolvedSearchParams.from);

  return (
    <SettingsClient
      user={user}
      forceSetupLocalPassword={setupLocalPassword && !user.hasLocalPassword}
      from={from}
    />
  );
}
