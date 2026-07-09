import { NextResponse } from "next/server";

export function GET() {
  return NextResponse.json({
    service: "termx-web-control",
    status: "ok",
    runtime: "control-plane",
    transport: "signaling-control-only",
  });
}
