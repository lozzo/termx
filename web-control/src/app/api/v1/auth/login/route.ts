import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { users } from "@/lib/schema";
import { verifyPassword } from "@/lib/password";
import { asError, createControlAccessToken } from "@/lib/termx-control";

const APP_LOGIN_ACCESS_TOKEN_TTL_MS = 90 * 24 * 60 * 60 * 1000;

export async function POST(request: NextRequest) {
  try {
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
      name: "TermX App password login",
      expiresAt: new Date(Date.now() + APP_LOGIN_ACCESS_TOKEN_TTL_MS),
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
  } catch (error) {
    console.error("TermX v1 auth login error:", error);
    return NextResponse.json(asError("login_failed", "login failed"), { status: 500 });
  }
}
