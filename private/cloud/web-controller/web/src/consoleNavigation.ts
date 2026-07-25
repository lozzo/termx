import {
  CreditCard,
  Gauge,
  KeyRound,
  Laptop,
  PackageOpen,
  ReceiptText,
  Rocket,
  Server,
  SlidersHorizontal,
  TicketPercent,
  UserRound,
  UserRoundCog,
  type LucideIcon,
} from "lucide-react";
import { OperatorWorkspaceModule } from "@/generated/cloudpb/cloud_management_pb";

/** AccountSection 是账号中心右侧内容的 URL 投影，不拥有账号或运营权限。 */
export type AccountSection = "overview" | "devices" | "plans" | "account";

/** OperatorModuleKey 是运营模块与 `/operator/*` URL 的稳定映射。 */
export type OperatorModuleKey = "users" | "agents" | "orders" | "subscriptions" | "plans" | "privileges" | "promotions" | "hubs" | "releases";

/** ConsoleNavigationItem 只描述统一侧栏的展示信息；可见性仍由后端 Workspace 投影决定。 */
export type ConsoleNavigationItem<Key extends string> = {
  key: Key;
  icon: LucideIcon;
};

/** accountNavigationItems 固定账号中心的信息架构顺序。 */
export const accountNavigationItems: ReadonlyArray<ConsoleNavigationItem<AccountSection>> = [
  { key: "overview", icon: Gauge },
  { key: "devices", icon: Laptop },
  { key: "plans", icon: CreditCard },
  { key: "account", icon: UserRound },
];

/** operatorNavigationItems 映射运营模块图标与后端权限枚举，不复制角色判断。 */
export const operatorNavigationItems: ReadonlyArray<ConsoleNavigationItem<OperatorModuleKey> & { permission: OperatorWorkspaceModule }> = [
  { key: "users", permission: OperatorWorkspaceModule.USERS, icon: UserRoundCog },
  { key: "agents", permission: OperatorWorkspaceModule.AGENTS, icon: Laptop },
  { key: "orders", permission: OperatorWorkspaceModule.ORDERS, icon: ReceiptText },
  { key: "subscriptions", permission: OperatorWorkspaceModule.SUBSCRIPTIONS, icon: KeyRound },
  { key: "plans", permission: OperatorWorkspaceModule.PLANS, icon: PackageOpen },
  { key: "privileges", permission: OperatorWorkspaceModule.PRIVILEGES, icon: SlidersHorizontal },
  { key: "promotions", permission: OperatorWorkspaceModule.PROMOTIONS, icon: TicketPercent },
  { key: "hubs", permission: OperatorWorkspaceModule.HUBS, icon: Server },
  { key: "releases", permission: OperatorWorkspaceModule.RELEASES, icon: Rocket },
];

/** operatorModuleFromPath 只解析 URL；调用方必须再用 Workspace 校验是否允许访问。 */
export function operatorModuleFromPath(pathname: string): OperatorModuleKey | undefined {
  const key = pathname.replace(/\/$/, "").split("/")[2];
  if (key === "catalog") return "plans";
  return operatorNavigationItems.some((module) => module.key === key) ? key as OperatorModuleKey : undefined;
}
