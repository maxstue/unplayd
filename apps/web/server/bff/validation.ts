import { Readable } from "node:stream";
import type { ReadableStream as NodeReadableStream } from "node:stream/web";
import { getHeader, getQuery, getRouterParam, type H3Event } from "h3";
import { abortable, InvalidInput, publicError } from "../clients/request";

const uuidPattern = /^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;
const ownerships = ["owned", "wishlist", "subscription"] as const;
const statuses = ["backlog", "playing", "paused", "completed", "dropped"] as const;
const kinds = ["game", "dlc", "expansion", "edition", "remake", "port", "mod", "episode"] as const;

function invalid(): never {
  throw new InvalidInput();
}
function text(value: unknown, min: number, max: number) {
  if (typeof value !== "string" || value.length < min || value.length > max) {
    return invalid();
  }
  return value;
}
function uuid(value: unknown) {
  const result = text(value, 36, 36);
  if (!uuidPattern.test(result)) {
    return invalid();
  }
  return result.toLowerCase();
}
function choice<T extends string>(value: unknown, options: readonly T[]) {
  if (!options.includes(value as T)) {
    return invalid();
  }
  return value as T;
}
function flag(value: unknown) {
  return value === "true" ? (true as const) : invalid();
}
function limit(value: unknown) {
  if (value === undefined) {
    return undefined;
  }
  const result = Number(text(value, 1, 2));
  if (!Number.isInteger(result) || result < 1 || result > 50) {
    return invalid();
  }
  return result;
}

/**
 * Reads and validates the `gameId` route parameter.
 *
 * @param event - The incoming {@link H3Event}.
 * @returns The lowercased UUID.
 * @throws {@link InvalidInput} if it is not a UUID.
 */
export function gameId(event: H3Event) {
  return uuid(getRouterParam(event, "gameId"));
}
/**
 * Validates the library list query (`status`, `ownership`, `platformId`, `favorite`, `limit`, `cursor`).
 *
 * @param event - The incoming {@link H3Event}.
 * @returns The validated query; absent parameters stay `undefined`.
 * @throws {@link InvalidInput} for any invalid parameter.
 */
export function libraryQuery(event: H3Event) {
  const q = getQuery(event);
  return {
    status: q.status === undefined ? undefined : choice(q.status, statuses),
    ownership: q.ownership === undefined ? undefined : choice(q.ownership, ownerships),
    platformId: q.platformId === undefined ? undefined : uuid(q.platformId),
    favorite: q.favorite === undefined ? undefined : flag(q.favorite),
    limit: limit(q.limit),
    cursor: q.cursor === undefined ? undefined : text(q.cursor, 1, 512),
  };
}
/**
 * Validates the catalog search query (`query`, `platformId`, `limit`, `cursor`, repeated `kind`).
 *
 * @param event - The incoming {@link H3Event}.
 * @returns The validated query.
 * @throws {@link InvalidInput} for any invalid parameter or duplicate `kind`.
 */
export function catalogQuery(event: H3Event) {
  const q = getQuery(event);
  const requestedKinds = Array.isArray(q.kind) ? q.kind : [q.kind];
  const kind =
    q.kind === undefined ? undefined : requestedKinds.map((value) => choice(value, kinds));
  if (kind && (kind.length > 8 || new Set(kind).size !== kind.length)) {
    return invalid();
  }
  return {
    query: text(q.query, 2, 200),
    platformId: q.platformId === undefined ? undefined : uuid(q.platformId),
    limit: limit(q.limit),
    cursor: q.cursor === undefined ? undefined : text(q.cursor, 1, 512),
    kind,
  };
}
/**
 * Validates the suggestion query, which allows only `query`.
 *
 * @param event - The incoming {@link H3Event}.
 * @returns The validated query.
 * @throws {@link InvalidInput} for a missing or invalid `query` or any extra parameter.
 */
export function suggestionQuery(event: H3Event) {
  const q = getQuery(event);
  if (Object.keys(q).some((key) => key !== "query")) {
    return invalid();
  }
  return { query: text(q.query, 2, 200) };
}
async function body(event: H3Event, fields: string[]) {
  if (getHeader(event, "content-type")?.split(";")[0] !== "application/json") {
    return invalid();
  }
  const maxBytes = 16 * 1024;
  if (Number(getHeader(event, "content-length")) > maxBytes) {
    throw publicError(413, event.context.requestId);
  }
  const webBody = event.web?.request?.body;
  const stream = webBody
    ? Readable.fromWeb(webBody as NodeReadableStream<Uint8Array>)
    : event.node.req;
  const signal = event.context.requestSignal as AbortSignal | undefined;
  const read = async () => {
    const chunks: Uint8Array[] = [];
    let size = 0;
    // Keep the HTTP connection writable long enough to return a 413.
    for await (const chunk of stream.iterator({
      destroyOnReturn: false,
    }) as AsyncIterable<Uint8Array>) {
      size += chunk.byteLength;
      if (size > maxBytes) {
        throw publicError(413, event.context.requestId);
      }
      chunks.push(chunk);
    }
    return chunks;
  };
  let chunks: Uint8Array[];
  try {
    // Bound the read without destroying the request, so the 503 can still be sent.
    chunks = await (signal ? abortable(read(), signal) : read());
  } finally {
    stream.resume();
  }
  let data: unknown;
  try {
    data = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    return invalid();
  }
  if (
    !data ||
    typeof data !== "object" ||
    Array.isArray(data) ||
    Object.keys(data).some((key) => !fields.includes(key))
  ) {
    return invalid();
  }
  return data as Record<string, unknown>;
}
/**
 * Reads and validates the JSON body for adding a library entry.
 *
 * @param event - The incoming {@link H3Event}.
 * @returns `gameId`, `ownership` and, if sent, `platformId` (`null` clears it).
 * @throws {@link InvalidInput} for invalid JSON or fields, a 413 {@link publicError} above 16 KiB.
 */
export async function addEntryBody(event: H3Event) {
  const data = await body(event, ["gameId", "ownership", "platformId"]);
  return {
    gameId: uuid(data.gameId),
    ownership: choice(data.ownership, ownerships),
    ...(data.platformId === undefined
      ? {}
      : { platformId: data.platformId === null ? null : uuid(data.platformId) }),
  };
}
/**
 * Reads and validates the JSON body for changing a library entry's status.
 *
 * @param event - The incoming {@link H3Event}.
 * @returns The validated `status`.
 * @throws {@link InvalidInput} for invalid JSON or fields, a 413 {@link publicError} above 16 KiB.
 */
export async function statusBody(event: H3Event) {
  return { status: choice((await body(event, ["status"])).status, statuses) };
}
/**
 * Reads and validates the JSON body for a catalog batch lookup.
 *
 * @param event - The incoming {@link H3Event}.
 * @returns 1 to 100 unique, lowercased game IDs.
 * @throws {@link InvalidInput} for invalid JSON or IDs, a 413 {@link publicError} above 16 KiB.
 */
export async function batchBody(event: H3Event) {
  const data = await body(event, ["gameIds"]);
  if (!Array.isArray(data.gameIds) || data.gameIds.length < 1 || data.gameIds.length > 100) {
    return invalid();
  }
  const gameIds = data.gameIds.map(uuid);
  if (new Set(gameIds).size !== gameIds.length) {
    return invalid();
  }
  return { gameIds };
}
