import { getHeader, getQuery, getRouterParam, readBody, type H3Event } from "h3";
import { InvalidInput } from "../clients/request";

const uuidPattern = /^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;
const ownerships = ["owned", "wishlist", "subscription"] as const;
const statuses = ["backlog", "playing", "paused", "completed", "dropped"] as const;
const kinds = ["game", "dlc", "expansion", "edition", "remake", "port", "mod", "episode"] as const;

function invalid(): never {
  throw new InvalidInput();
}
function text(value: unknown, min: number, max: number) {
  if (typeof value !== "string" || value.length < min || value.length > max) return invalid();
  return value;
}
function uuid(value: unknown) {
  const result = text(value, 36, 36);
  if (!uuidPattern.test(result)) return invalid();
  return result.toLowerCase();
}
function choice<T extends string>(value: unknown, options: readonly T[]) {
  if (!options.includes(value as T)) return invalid();
  return value as T;
}
function limit(value: unknown) {
  if (value === undefined) return undefined;
  const result = Number(text(value, 1, 2));
  if (!Number.isInteger(result) || result < 1 || result > 50) return invalid();
  return result;
}

export function gameId(event: H3Event) {
  return uuid(getRouterParam(event, "gameId"));
}
export function libraryQuery(event: H3Event) {
  const q = getQuery(event);
  return {
    status: q.status === undefined ? undefined : choice(q.status, statuses),
    ownership: q.ownership === undefined ? undefined : choice(q.ownership, ownerships),
    platformId: q.platformId === undefined ? undefined : uuid(q.platformId),
    limit: limit(q.limit),
    cursor: q.cursor === undefined ? undefined : text(q.cursor, 1, 512),
  };
}
export function catalogQuery(event: H3Event) {
  const q = getQuery(event);
  const kind =
    q.kind === undefined
      ? undefined
      : (Array.isArray(q.kind) ? q.kind : [q.kind]).map((value) => choice(value, kinds));
  if (kind && (kind.length > 8 || new Set(kind).size !== kind.length)) return invalid();
  return {
    query: text(q.query, 2, 200),
    platformId: q.platformId === undefined ? undefined : uuid(q.platformId),
    limit: limit(q.limit),
    cursor: q.cursor === undefined ? undefined : text(q.cursor, 1, 512),
    kind,
  };
}
async function body(event: H3Event, fields: string[]) {
  if (getHeader(event, "content-type")?.split(";")[0] !== "application/json") return invalid();
  let data: unknown;
  try {
    data = await readBody(event);
  } catch {
    return invalid();
  }
  if (
    !data ||
    typeof data !== "object" ||
    Array.isArray(data) ||
    Object.keys(data).some((key) => !fields.includes(key))
  )
    return invalid();
  return data as Record<string, unknown>;
}
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
export async function statusBody(event: H3Event) {
  return { status: choice((await body(event, ["status"])).status, statuses) };
}
export async function batchBody(event: H3Event) {
  const data = await body(event, ["gameIds"]);
  if (!Array.isArray(data.gameIds) || data.gameIds.length < 1 || data.gameIds.length > 100)
    return invalid();
  const gameIds = data.gameIds.map(uuid);
  if (new Set(gameIds).size !== gameIds.length) return invalid();
  return { gameIds };
}
