import {
  S3Client,
  PutObjectCommand,
} from "@aws-sdk/client-s3";
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";

// ========== 主 S3 (洛杉矶) ==========

function getS3Client() {
  return new S3Client({
    region: process.env.S3_REGION || "auto",
    endpoint: process.env.S3_ENDPOINT,
    credentials: {
      accessKeyId: process.env.S3_ACCESS_KEY!,
      secretAccessKey: process.env.S3_SECRET_KEY!,
    },
    forcePathStyle: true,
  });
}

function getBucket() {
  return process.env.S3_BUCKET!;
}

export async function uploadFile(
  key: string,
  buffer: Buffer,
  contentType: string
): Promise<void> {
  const client = getS3Client();
  await client.send(
    new PutObjectCommand({
      Bucket: getBucket(),
      Key: key,
      Body: buffer,
      ContentType: contentType,
    })
  );
}

export async function getPresignedUploadUrl(
  key: string,
  contentType: string
): Promise<string> {
  const client = getS3Client();
  const command = new PutObjectCommand({
    Bucket: getBucket(),
    Key: key,
    ContentType: contentType,
  });
  return getSignedUrl(client, command, { expiresIn: 900 });
}

export function getDownloadUrl(key: string): string {
  const endpoint = process.env.S3_ENDPOINT!;
  const bucket = getBucket();
  return `${endpoint}/${bucket}/${key}`;
}

export function getDownloadMirrors(key: string): string[] {
  const urls: string[] = [];
  const primary = getDownloadUrl(key);
  urls.push(primary);

  const lbEndpoint = process.env.S3_LB_ENDPOINT;
  const lbBucket = process.env.S3_LB_BUCKET;
  if (lbEndpoint && lbBucket) {
    urls.push(`${lbEndpoint}/${lbBucket}/${key}`);
  }
  return urls;
}

// ========== 阿里云 OSS (S3 兼容) ==========

function getOSSClient(): S3Client | null {
  const endpoint = process.env.OSS_ENDPOINT;
  const ak = process.env.OSS_ACCESS_KEY;
  const sk = process.env.OSS_SECRET_KEY;
  if (!endpoint || !ak || !sk) return null;

  return new S3Client({
    region: process.env.OSS_REGION || "auto",
    endpoint: `https://${endpoint}`,
    credentials: { accessKeyId: ak, secretAccessKey: sk },
    forcePathStyle: false, // OSS 使用虚拟主机风格
  });
}

function getOSSBucket(): string {
  return process.env.OSS_BUCKET || "";
}

export function ossEnabled(): boolean {
  return !!(process.env.OSS_ENDPOINT && process.env.OSS_ACCESS_KEY && process.env.OSS_BUCKET);
}

export async function uploadToOSS(
  key: string,
  buffer: Buffer,
  contentType: string
): Promise<string | null> {
  const client = getOSSClient();
  const bucket = getOSSBucket();
  if (!client || !bucket) return null;

  await client.send(
    new PutObjectCommand({
      Bucket: bucket,
      Key: key,
      Body: buffer,
      ContentType: contentType,
    })
  );

  const endpoint = process.env.OSS_ENDPOINT!;
  return `https://${bucket}.${endpoint}/${key}`;
}

// ========== GitHub Releases ==========

const GITHUB_REPO = process.env.GITHUB_REPO || ""; // e.g. "lozzo/termx-releases"
const GITHUB_TOKEN = process.env.GITHUB_TOKEN || "";

export function githubEnabled(): boolean {
  return !!(GITHUB_REPO && GITHUB_TOKEN);
}

export async function uploadToGitHub(
  version: string,
  fileName: string,
  buffer: Buffer
): Promise<string | null> {
  if (!GITHUB_REPO || !GITHUB_TOKEN) return null;

  const tag = `v${version}`;
  const [owner, repo] = GITHUB_REPO.split("/");
  const headers = {
    Authorization: `token ${GITHUB_TOKEN}`,
    Accept: "application/vnd.github.v3+json",
    "User-Agent": "termx-release",
  };

  // 1. 获取或创建 release
  let releaseId: number;
  const getResp = await fetch(
    `https://api.github.com/repos/${owner}/${repo}/releases/tags/${tag}`,
    { headers }
  );

  if (getResp.ok) {
    const data = await getResp.json();
    releaseId = data.id;
  } else {
    // 创建 release
    const createResp = await fetch(
      `https://api.github.com/repos/${owner}/${repo}/releases`,
      {
        method: "POST",
        headers: { ...headers, "Content-Type": "application/json" },
        body: JSON.stringify({
          tag_name: tag,
          name: tag,
          body: `Release ${tag}`,
          draft: false,
          prerelease: false,
        }),
      }
    );
    if (!createResp.ok) {
      console.error(`[github] create release failed: ${createResp.status}`);
      return null;
    }
    const data = await createResp.json();
    releaseId = data.id;
  }

  // 2. 删除同名 asset (如果存在)
  const assetsResp = await fetch(
    `https://api.github.com/repos/${owner}/${repo}/releases/${releaseId}/assets`,
    { headers }
  );
  if (assetsResp.ok) {
    const assets = await assetsResp.json();
    for (const asset of assets) {
      if (asset.name === fileName) {
        await fetch(
          `https://api.github.com/repos/${owner}/${repo}/releases/assets/${asset.id}`,
          { method: "DELETE", headers }
        );
        break;
      }
    }
  }

  // 3. 上传 asset
  const uploadUrl = `https://uploads.github.com/repos/${owner}/${repo}/releases/${releaseId}/assets?name=${encodeURIComponent(fileName)}`;
  const uploadResp = await fetch(uploadUrl, {
    method: "POST",
    headers: {
      ...headers,
      "Content-Type": "application/octet-stream",
      "Content-Length": String(buffer.length),
    },
    body: new Uint8Array(buffer),
  });

  if (!uploadResp.ok) {
    console.error(`[github] upload asset failed: ${uploadResp.status}`);
    return null;
  }

  return `https://github.com/${GITHUB_REPO}/releases/download/${tag}/${fileName}`;
}
