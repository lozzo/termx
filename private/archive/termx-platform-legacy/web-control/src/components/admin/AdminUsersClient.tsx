"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

interface UserItem {
  id: string;
  username: string;
  email: string;
  role: string;
  createdAt: string;
  subscriptionStatus: string | null;
  subscriptionEnd: string | null;
  agentCount: number;
}

export default function AdminUsersClient({ initialUsers }: { initialUsers: UserItem[] }) {
  const [users, setUsers] = useState<UserItem[]>(initialUsers);

  const fetchUsers = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/users");
      if (res.ok) setUsers(await res.json());
    } catch { /* */ }
  }, []);

  const handleToggleRole = async (id: string, currentRole: string) => {
    const newRole = currentRole === "admin" ? "user" : "admin";
    const res = await fetch(`/api/admin/users/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ role: newRole }),
    });
    if (!res.ok) {
      const data = await res.json();
      alert(data.error || "操作失败");
      return;
    }
    fetchUsers();
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-3xl font-bold tracking-tight">用户管理</h2>
        <p className="text-zinc-400">查看和管理所有注册用户。</p>
      </div>

      <Card className="bg-zinc-900 border-zinc-800">
        <CardHeader>
          <CardTitle className="text-base">全部用户 ({users.length})</CardTitle>
        </CardHeader>
        <CardContent>
          {users.length === 0 ? (
            <p className="text-center text-zinc-500 py-8">暂无用户</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800 text-zinc-400">
                    <th className="text-left py-3 px-2 font-medium">用户名</th>
                    <th className="text-left py-3 px-2 font-medium">邮箱</th>
                    <th className="text-left py-3 px-2 font-medium">角色</th>
                    <th className="text-left py-3 px-2 font-medium">订阅状态</th>
                    <th className="text-left py-3 px-2 font-medium">节点数</th>
                    <th className="text-left py-3 px-2 font-medium">注册时间</th>
                    <th className="text-right py-3 px-2 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((u) => (
                    <tr key={u.id} className="border-b border-zinc-800/50">
                      <td className="py-3 px-2 font-medium text-white">{u.username}</td>
                      <td className="py-3 px-2 text-zinc-400">{u.email}</td>
                      <td className="py-3 px-2">
                        <Badge
                          variant="outline"
                          className={u.role === "admin" ? "border-primary text-primary" : "border-zinc-600 text-zinc-500"}
                        >
                          {u.role}
                        </Badge>
                      </td>
                      <td className="py-3 px-2">
                        {u.subscriptionStatus ? (
                          <Badge
                            variant="outline"
                            className={u.subscriptionStatus === "active" ? "border-green-600 text-green-500" : "border-zinc-600 text-zinc-500"}
                          >
                            {u.subscriptionStatus === "active" ? "活跃" : u.subscriptionStatus}
                          </Badge>
                        ) : (
                          <span className="text-zinc-600">无订阅</span>
                        )}
                      </td>
                      <td className="py-3 px-2 text-zinc-300">{u.agentCount}</td>
                      <td className="py-3 px-2 text-zinc-400 text-xs">
                        {new Date(u.createdAt).toLocaleDateString()}
                      </td>
                      <td className="py-3 px-2 text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleToggleRole(u.id, u.role)}
                          className="text-xs"
                        >
                          {u.role === "admin" ? "设为用户" : "设为管理员"}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
