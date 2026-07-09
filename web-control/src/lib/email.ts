import { Resend } from "resend";
import Mailgun from "mailgun.js";
import FormData from "form-data";

// ========== 邮件提供商抽象 ==========

interface SendMailOptions {
  to: string;
  subject: string;
  html: string;
}

interface MailProvider {
  send(options: SendMailOptions): Promise<void>;
}

// ---------- Resend ----------
function createResendProvider(): MailProvider {
  const resend = new Resend(process.env.RESEND_API_KEY);
  return {
    async send({ to, subject, html }) {
      await resend.emails.send({ from: FROM_EMAIL, to, subject, html });
    },
  };
}

// ---------- Mailgun ----------
function createMailgunProvider(): MailProvider {
  const mg = new Mailgun(FormData);
  const client = mg.client({
    username: "api",
    key: process.env.MAILGUN_API_KEY!,
    // 欧洲区域使用: url: "https://api.eu.mailgun.net"
    ...(process.env.MAILGUN_API_URL ? { url: process.env.MAILGUN_API_URL } : {}),
  });
  const domain = process.env.MAILGUN_DOMAIN!;
  return {
    async send({ to, subject, html }) {
      await client.messages.create(domain, { from: FROM_EMAIL, to: [to], subject, html });
    },
  };
}

// ========== 初始化 ==========

const FROM_EMAIL = process.env.EMAIL_FROM || "noreply@termx.omscd.com";

const providers: Record<string, () => MailProvider> = {
  resend: createResendProvider,
  mailgun: createMailgunProvider,
};

function getMailProvider(): MailProvider {
  const name = (process.env.EMAIL_PROVIDER || "resend").toLowerCase();
  const factory = providers[name];
  if (!factory) {
    throw new Error(`Unknown EMAIL_PROVIDER: ${name}. Supported: ${Object.keys(providers).join(", ")}`);
  }
  return factory();
}

let _provider: MailProvider | null = null;
function provider(): MailProvider {
  if (!_provider) _provider = getMailProvider();
  return _provider;
}

// ========== 业务邮件 ==========

export async function sendResetCode(email: string, code: string) {
  await provider().send({
    to: email,
    subject: "重置密码验证码 - termx",
    html: `
      <div style="font-family: sans-serif; max-width: 480px; margin: 0 auto; padding: 32px;">
        <h2 style="color: #111; margin-bottom: 16px;">重置密码</h2>
        <p style="color: #555; font-size: 14px;">您正在重置 termx 账户密码，验证码如下：</p>
        <div style="background: #f4f4f5; border-radius: 8px; padding: 20px; text-align: center; margin: 24px 0;">
          <span style="font-size: 32px; font-weight: bold; letter-spacing: 8px; color: #111;">${code}</span>
        </div>
        <p style="color: #555; font-size: 14px;">验证码有效期为 10 分钟。如果您没有请求重置密码，请忽略此邮件。</p>
      </div>
    `,
  });
}
