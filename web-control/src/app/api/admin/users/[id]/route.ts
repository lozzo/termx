import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { users } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq } from "drizzle-orm";

export async function PATCH(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const { id } = await params;
  const body = await request.json();

  if (id === admin.userId) {
    return NextResponse.json({ error: "不能修改自己的角色" }, { status: 400 });
  }

  if (body.role !== "user" && body.role !== "admin") {
    return NextResponse.json({ error: "role must be 'user' or 'admin'" }, { status: 400 });
  }

  const [updated] = await db
    .update(users)
    .set({ role: body.role })
    .where(eq(users.id, id))
    .returning();

  if (!updated) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  return NextResponse.json({ id: updated.id, role: updated.role });
}
