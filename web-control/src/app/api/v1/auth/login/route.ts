import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { users } from "@/lib/schema";
import { verifyPassword } from "@/lib/password";
import { asError, createControlAccessToken } from "@/lib/termx-control";

export async function POST(request: NextRequest) {
  const body = await request.json();
  const login = typeof body.email === "string" ? body.email.trim() : typeof body.username === "string" ? body.username.trim() : "";
  const password = typeof body.password === "string" ? body.password : "";
  if (!login || !password) {
    return NextResponse.json(asError("invalid_login", "email/username and password are required"), { status: 400 });
  }

  const [user] = await db
    .select()
    .from(users)
    .where(login.includes("@") ? eq(users.email, login) : eq(users.username, login))
    .limit(1);

  if (!user?.passwordHash || !(await verifyPassword(password, user.passwordHash))) {
    return NextResponse.json(asError("invalid_credentials", "invalid email/username or password"), { status: 401 });
  }

  const accessToken = await createControlAccessToken({
    userId: user.id,
    name: "TermX CLI password login",
    expiresAt: null,
  });

  return NextResponse.json({
    token_type: "Bearer",
    access_token: accessToken.token,
    refresh_token: "",
    user: {
      id: user.id,
      username: user.username,
      email: user.email,
      role: user.role,
    },
  });
}
