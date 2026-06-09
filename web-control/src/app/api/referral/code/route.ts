import { NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { getOrCreateReferralCode } from "@/lib/referral.service";

export async function GET(request: Request) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "未登录" }, { status: 401 });
  }

  const referralCode = await getOrCreateReferralCode(user.userId);

  return NextResponse.json({ referralCode });
}
