import {
  fromJson,
  toJson,
  type DescMessage,
  type JsonValue,
  type MessageShape,
} from "@bufbuild/protobuf";

export class ProtoHTTPError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

function cookie(name: string): string {
  return (
    document.cookie
      .split("; ")
      .find((value) => value.startsWith(`${name}=`))
      ?.slice(name.length + 1) ?? ""
  );
}

async function decode<Desc extends DescMessage>(
  response: Response,
  schema: Desc,
): Promise<MessageShape<Desc>> {
  const text = await response.text();
  const json = text ? (JSON.parse(text) as JsonValue) : {};
  if (!response.ok) {
    const detail = json as { message?: string; code?: string };
    throw new ProtoHTTPError(
      response.status,
      detail.message ?? detail.code ?? `HTTP ${response.status}`,
    );
  }
  return fromJson(schema, json);
}

export async function protoGet<Res extends DescMessage>(
  path: string,
  responseSchema: Res,
): Promise<MessageShape<Res>> {
  return decode(
    await fetch(path, { cache: "no-store", credentials: "same-origin" }),
    responseSchema,
  );
}

export async function protoPost<
  Req extends DescMessage,
  Res extends DescMessage,
>(
  path: string,
  requestSchema: Req,
  request: MessageShape<Req>,
  responseSchema: Res,
  csrfCookie = "muxvia_cloud_csrf",
): Promise<MessageShape<Res>> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  const proof = cookie(csrfCookie);
  if (proof) headers["X-Muxvia-CSRF"] = proof;
  const response = await fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers,
    body: JSON.stringify(
      toJson(requestSchema, request, { useProtoFieldName: true }),
    ),
  });
  return decode(response, responseSchema);
}
