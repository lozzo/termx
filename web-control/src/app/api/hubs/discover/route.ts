import { NextResponse } from "next/server";

// Deprecated: apps should use GET /api/v1/machines and read per-machine hub_urls.
export async function GET() {
  return NextResponse.json(
    {
      error: "deprecated_endpoint",
      message: "use /api/v1/machines for the user-scoped agent directory",
    },
    { status: 410 }
  );
}
