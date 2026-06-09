import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { users } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { eq, and, not, or } from "drizzle-orm";

export async function PUT(request: NextRequest) {
  try {
    const jwtUser = await getAuthFromRequest(request);
    if (!jwtUser) {
      return NextResponse.json({ error: "未登录" }, { status: 401 });
    }

    const body = await request.json();
    const { email, username } = body;

    if (!email || !username) {
      return NextResponse.json({ error: "邮箱和用户名为必填项" }, { status: 400 });
    }

    const [currentUser] = await db
      .select({
        id: users.id,
        email: users.email,
        githubId: users.githubId,
      })
      .from(users)
      .where(eq(users.id, jwtUser.userId))
      .limit(1);

    if (!currentUser) {
      return NextResponse.json({ error: "用户不存在" }, { status: 404 });
    }

    if (currentUser.githubId && email !== currentUser.email) {
      return NextResponse.json(
        { error: "GitHub 登录账户的邮箱暂不支持修改" },
        { status: 400 }
      );
    }

    const nextEmail = currentUser.githubId ? currentUser.email : email;

    // 检查用户名/邮箱是否被其他人占用
    const [conflict] = await db
      .select()
      .from(users)
      .where(
        and(
          not(eq(users.id, jwtUser.userId)),
          or(eq(users.username, username), eq(users.email, nextEmail))
        )
      )
      .limit(1);

    if (conflict) {
      const field = conflict.username === username ? "用户名" : "邮箱";
      return NextResponse.json({ error: `该${field}已被占用` }, { status: 409 });
    }

    const [user] = await db
      .update(users)
      .set({ email: nextEmail, username })
      .where(eq(users.id, jwtUser.userId))
      .returning({
        id: users.id,
        username: users.username,
        email: users.email,
        role: users.role,
      });

    return NextResponse.json({ user });
  } catch (error) {
    console.error("Update profile error:", error);
    return NextResponse.json({ error: "更新失败" }, { status: 500 });
  }
}
