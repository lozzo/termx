import { OmsPayClient } from "./oms-pay-sdk";

let client: OmsPayClient | null = null;

export function getOmsPayClient(): OmsPayClient {
  if (client) return client;

  const baseUrl = process.env.OMS_PAY_BASE_URL;
  const apiKey = process.env.OMS_PAY_API_KEY;

  if (!baseUrl || !apiKey) {
    throw new Error(
      "OMS-Pay is not configured. Set OMS_PAY_BASE_URL and OMS_PAY_API_KEY environment variables."
    );
  }

  client = new OmsPayClient({ baseUrl, apiKey });
  console.log("[OMS-Pay] Client initialized:", baseUrl);
  return client;
}

export function isOmsPayConfigured(): boolean {
  return !!(process.env.OMS_PAY_BASE_URL && process.env.OMS_PAY_API_KEY);
}

export function getOmsPayWebhookSecret(): string {
  const secret = process.env.OMS_PAY_WEBHOOK_SECRET;
  if (!secret) {
    throw new Error(
      "OMS_PAY_WEBHOOK_SECRET is not configured."
    );
  }
  return secret;
}
