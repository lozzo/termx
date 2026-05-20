import { NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { users } from "@/lib/schema";
import { asError, requireAccessToken } from "@/lib/termx-control";

export async function GET(request: Request) {
  const tokenRecord = await requireAccessToken(request);
  if (!tokenRecord) {
    return NextResponse.json(asError("unauthorized", "valid bearer access token is required"), { status: 401 });
  }

  const [user] = await db
    .select({
      id: users.id,
      username: users.username,
      email: users.email,
      role: users.role,
    })
    .from(users)
    .where(eq(users.id, tokenRecord.userId))
    .limit(1);

  if (!user) {
    return NextResponse.json(asError("user_not_found", "user was not found"), { status: 404 });
  }

  return NextResponse.json({
    user,
  });
}
