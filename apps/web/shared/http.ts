/** Reads a positive Retry-After delay in seconds. */
export function retryAfterSeconds(headers: Headers): number | undefined {
  const value = headers.get("Retry-After");
  if (value === null || !/^\d+$/.test(value)) {
    return;
  }
  const seconds = Number(value);
  if (Number.isSafeInteger(seconds) && seconds > 0) {
    return seconds;
  }
}
