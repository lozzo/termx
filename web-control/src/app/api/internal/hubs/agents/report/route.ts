import { NextResponse } from "next/server";

// Deprecated: Hub agent state is synchronized by /api/internal/hubs/heartbeat.
// This endpoint must not write durable agent ownership from Hub-supplied claims.
export async function POST() {
  return NextResponse.json(
    {
      error: "deprecated_endpoint",
      message: "agent reports are handled by /api/internal/hubs/heartbeat",
    },
    { status: 410 }
  );
}
