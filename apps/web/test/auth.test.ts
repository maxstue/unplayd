import { randomUUID } from "node:crypto";
import { createApp, defineEventHandler, getHeader, toWebHandler, type H3Event } from "h3";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { currentUser } from "../server/auth/current-user";
import { requireSession } from "../server/auth/session";
import requestId from "../server/middleware/request-id";

const config = { appOrigin: "https://app.example", userApiUrl: "http://user-api:8445" };

function clerkSession(userId = "user_alice") {
  return {
    isAuthenticated: true,
    userId: userId as string | null,
    sessionId: `session_${userId}` as string | null,
    getToken: vi.fn<() => Promise<string | null>>().mockResolvedValue(randomUUID()),
  };
}

type Session = ReturnType<typeof clerkSession>;

function sessionApp(auth: ReturnType<typeof vi.fn>) {
  return toWebHandler(
    createApp()
      .use(
        defineEventHandler((event) => {
          Object.assign(event.context, { auth });
        }),
      )
      .use(
        "/api/private",
        defineEventHandler((event) => {
          const session = requireSession(event, config.appOrigin);
          return { userId: session.userId, sessionId: session.sessionId };
        }),
      ),
  );
}

function currentUserApp(session: Session | ((event: H3Event) => Session), fetcher: typeof fetch) {
  return toWebHandler(
    createApp()
      .use(requestId)
      .use(
        defineEventHandler((event) => {
          const auth = typeof session === "function" ? session(event) : session;
          Object.assign(event.context, { auth: vi.fn(() => auth) });
        }),
      )
      .use(
        "/api/me",
        defineEventHandler((event) => currentUser(event, config, fetcher)),
      ),
  );
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>().mockRejectedValue(new Error("Unexpected external fetch")),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("requireSession through H3", () => {
  it("accepts only a Clerk session token and marks the response private", async () => {
    const session = clerkSession();
    const auth = vi.fn(() => session);
    const response = await sessionApp(auth)(new Request(`${config.appOrigin}/api/private`));

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ userId: session.userId, sessionId: session.sessionId });
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(auth).toHaveBeenCalledExactlyOnceWith({ acceptsToken: "session_token" });
    expect(session.getToken).not.toHaveBeenCalled();
  });

  it.each([
    { name: "missing Clerk context", session: undefined },
    { name: "unauthenticated session", session: { ...clerkSession(), isAuthenticated: false } },
    { name: "missing user ID", session: { ...clerkSession(), userId: null } },
    { name: "empty user ID", session: { ...clerkSession(), userId: "" } },
    { name: "missing session ID", session: { ...clerkSession(), sessionId: null } },
    { name: "empty session ID", session: { ...clerkSession(), sessionId: "" } },
  ])("rejects $name even when browser identity headers are present", async ({ session }) => {
    const auth = vi.fn(() => session);
    const response = await sessionApp(auth)(
      new Request(`${config.appOrigin}/api/private`, {
        headers: { Authorization: `Bearer ${randomUUID()}`, "X-User-ID": "user_forged" },
      }),
    );

    expect(response.status).toBe(401);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(await response.json()).toMatchObject({ statusCode: 401, statusMessage: "Unauthorized" });
  });

  it("rejects requests with no Clerk auth function", async () => {
    const app = createApp().use(
      defineEventHandler((event) => requireSession(event, config.appOrigin)),
    );
    const response = await toWebHandler(app)(new Request(`${config.appOrigin}/api/private`));

    expect(response.status).toBe(401);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
  });

  it.each(["GET", "HEAD", "OPTIONS"])(
    "allows a session-authenticated %s without an Origin",
    async (method) => {
      const auth = vi.fn(() => clerkSession());
      const response = await sessionApp(auth)(
        new Request(`${config.appOrigin}/api/private`, { method }),
      );

      expect(response.status).toBe(200);
      expect(response.headers.get("Cache-Control")).toBe("no-store");
      expect(auth).toHaveBeenCalledExactlyOnceWith({ acceptsToken: "session_token" });
    },
  );

  it.each(["POST", "PUT", "PATCH", "DELETE", "PROPFIND"])(
    "allows %s only with the exact configured Origin",
    async (method) => {
      const response = await sessionApp(vi.fn(() => clerkSession()))(
        new Request(`${config.appOrigin}/api/private`, {
          method,
          headers: { Origin: config.appOrigin, "Sec-Fetch-Site": "same-origin" },
        }),
      );

      expect(response.status).toBe(200);
      expect(response.headers.get("Cache-Control")).toBe("no-store");
    },
  );

  it.each(["POST", "PUT", "PATCH", "DELETE", "PROPFIND"])(
    "rejects %s without an Origin before consulting Clerk",
    async (method) => {
      const auth = vi.fn(() => clerkSession());
      const response = await sessionApp(auth)(
        new Request(`${config.appOrigin}/api/private`, { method }),
      );

      expect(response.status).toBe(403);
      expect(response.headers.get("Cache-Control")).toBe("no-store");
      expect(auth).not.toHaveBeenCalled();
    },
  );

  it.each<{ name: string; headers: Record<string, string> }>([
    { name: "null Origin", headers: { Origin: "null" } },
    { name: "foreign Origin", headers: { Origin: "https://attacker.example" } },
    { name: "different scheme", headers: { Origin: "http://app.example" } },
    { name: "different port", headers: { Origin: "https://app.example:8443" } },
    { name: "Origin with a trailing slash", headers: { Origin: `${config.appOrigin}/` } },
    {
      name: "cross-site fetch despite matching Origin",
      headers: { Origin: config.appOrigin, "Sec-Fetch-Site": "cross-site" },
    },
    {
      name: "forged Host",
      headers: { Origin: "https://attacker.example", Host: "attacker.example" },
    },
    {
      name: "forged proxy headers",
      headers: {
        Origin: "https://attacker.example",
        "X-Forwarded-Host": "attacker.example",
        "X-Forwarded-Proto": "https",
      },
    },
  ])("rejects mutation with $name", async ({ headers }) => {
    const auth = vi.fn(() => clerkSession());
    const response = await sessionApp(auth)(
      new Request(`${config.appOrigin}/api/private`, {
        method: "POST",
        headers: new Headers(headers),
      }),
    );

    expect(response.status).toBe(403);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(await response.json()).toMatchObject({ statusCode: 403, statusMessage: "Forbidden" });
    expect(auth).not.toHaveBeenCalled();
  });
});

describe("currentUser BFF through H3", () => {
  it("forwards only the server session token and correlation ID to Ktor /me", async () => {
    const session = clerkSession();
    const token = await session.getToken();
    session.getToken.mockClear();
    const user = { id: randomUUID(), clerkSubject: session.userId };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(Response.json(user));
    const id = randomUUID();
    const response = await currentUserApp(
      session,
      fetcher,
    )(
      new Request(`${config.appOrigin}/api/me?userId=user_forged`, {
        headers: {
          Authorization: `Bearer ${randomUUID()}`,
          Cookie: `browser_private=${randomUUID()}`,
          "X-User-ID": "user_forged",
          "X-Clerk-User-ID": "user_forged",
          "X-Request-ID": id,
        },
      }),
    );

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual(user);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(response.headers.get("X-Request-ID")).toBe(id);
    expect(session.getToken).toHaveBeenCalledExactlyOnceWith();
    expect(fetcher).toHaveBeenCalledOnce();
    const outgoing = fetcher.mock.calls[0]![0] as Request;
    expect(outgoing.url).toBe(`${config.userApiUrl}/me`);
    expect(outgoing.method).toBe("GET");
    expect(Object.fromEntries(outgoing.headers)).toEqual({
      authorization: `Bearer ${token}`,
      "x-request-id": id,
    });
    expect(await outgoing.text()).toBe("");
  });

  it("never obtains a token or calls Ktor for an unauthenticated session", async () => {
    const session = { ...clerkSession(), isAuthenticated: false };
    const fetcher = vi.fn<typeof fetch>();
    const response = await currentUserApp(
      session,
      fetcher,
    )(new Request(`${config.appOrigin}/api/me`));

    expect(response.status).toBe(401);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(session.getToken).not.toHaveBeenCalled();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it.each([null, ""])(
    "returns 401 without calling Ktor when getToken returns %j",
    async (token) => {
      const session = clerkSession();
      session.getToken.mockResolvedValue(token);
      const fetcher = vi.fn<typeof fetch>();
      const response = await currentUserApp(
        session,
        fetcher,
      )(new Request(`${config.appOrigin}/api/me`));

      expect(response.status).toBe(401);
      expect(response.headers.get("Cache-Control")).toBe("no-store");
      expect(await response.json()).toMatchObject({
        statusCode: 401,
        statusMessage: "Unauthorized",
      });
      expect(fetcher).not.toHaveBeenCalled();
    },
  );

  it("sanitizes a rejected Clerk token request without calling Ktor", async () => {
    const session = clerkSession();
    const privateDetail = `private-clerk-${randomUUID()}`;
    session.getToken.mockRejectedValue(new Error(privateDetail));
    const fetcher = vi.fn<typeof fetch>();
    const response = await currentUserApp(
      session,
      fetcher,
    )(new Request(`${config.appOrigin}/api/me`));

    expect(response.status).toBe(503);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    const body = await response.text();
    expect(JSON.parse(body)).toMatchObject({
      statusCode: 503,
      statusMessage: "User service unavailable",
    });
    expect(body).not.toContain(privateDetail);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it.each(["Clerk", "transport"])(
    "sanitizes a %s exception that carries its own 401 status and private details",
    async (source) => {
      const detail = `private-exception-${randomUUID()}`;
      const error = Object.assign(new Error(detail), {
        statusCode: 401,
        statusMessage: detail,
        data: { private: detail },
      });
      const session = clerkSession();
      const fetcher = vi.fn<typeof fetch>();
      if (source === "Clerk") session.getToken.mockRejectedValue(error);
      else fetcher.mockRejectedValue(error);
      const response = await currentUserApp(
        session,
        fetcher,
      )(new Request(`${config.appOrigin}/api/me`));

      expect(response.status).toBe(503);
      expect(response.headers.get("Cache-Control")).toBe("no-store");
      const body = await response.text();
      expect(JSON.parse(body)).toMatchObject({
        statusCode: 503,
        statusMessage: "User service unavailable",
      });
      expect(body).not.toContain(detail);
    },
  );

  it.each([
    { name: "upstream unauthorized", status: 401, expected: 401 },
    { name: "upstream forbidden", status: 403, expected: 503 },
    { name: "upstream internal failure", status: 500, expected: 503 },
    { name: "upstream unavailable", status: 503, expected: 503 },
    { name: "empty upstream success", status: 204, expected: 503 },
  ])("sanitizes $name", async ({ status, expected }) => {
    const detail = `private-upstream-${randomUUID()}`;
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValue(
        status === 204
          ? new Response(null, { status })
          : Response.json({ message: detail }, { status }),
      );
    const response = await currentUserApp(
      clerkSession(),
      fetcher,
    )(new Request(`${config.appOrigin}/api/me`));

    expect(response.status).toBe(expected);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    const body = await response.text();
    expect(JSON.parse(body)).toMatchObject({
      statusCode: expected,
      statusMessage: expected === 401 ? "Unauthorized" : "User service unavailable",
    });
    expect(body).not.toContain(detail);
    expect(body).not.toContain(config.userApiUrl);
  });

  it.each(["network failure", "malformed upstream JSON"])("sanitizes %s", async (failure) => {
    const detail = `private-network-${randomUUID()}`;
    const fetcher = vi.fn<typeof fetch>();
    if (failure === "network failure") fetcher.mockRejectedValue(new Error(detail));
    else
      fetcher.mockResolvedValue(
        new Response(detail, { headers: { "Content-Type": "application/json" } }),
      );
    const response = await currentUserApp(
      clerkSession(),
      fetcher,
    )(new Request(`${config.appOrigin}/api/me`));

    expect(response.status).toBe(503);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    const body = await response.text();
    expect(JSON.parse(body)).toMatchObject({
      statusCode: 503,
      statusMessage: "User service unavailable",
    });
    expect(body).not.toContain(detail);
  });

  it("isolates tokens, users, and request IDs across two interleaved requests", async () => {
    const alice = clerkSession("user_alice");
    const bob = clerkSession("user_bob");
    const aliceToken = randomUUID();
    const bobToken = randomUUID();
    let releaseAlice!: (token: string) => void;
    alice.getToken.mockImplementation(
      () =>
        new Promise<string>((resolve) => {
          releaseAlice = resolve;
        }),
    );
    bob.getToken.mockResolvedValue(bobToken);
    const users = {
      [aliceToken]: { id: randomUUID(), clerkSubject: alice.userId },
      [bobToken]: { id: randomUUID(), clerkSubject: bob.userId },
    };
    const fetcher = vi.fn<typeof fetch>().mockImplementation(async (input) => {
      const request = input as Request;
      const token = request.headers.get("Authorization")!.slice("Bearer ".length);
      return Response.json(users[token]);
    });
    const app = currentUserApp(
      (event) => (getHeader(event, "X-Request-ID") === "request_alice" ? alice : bob),
      fetcher,
    );
    const aliceResponse = app(
      new Request(`${config.appOrigin}/api/me`, { headers: { "X-Request-ID": "request_alice" } }),
    );
    const bobResponse = await app(
      new Request(`${config.appOrigin}/api/me`, { headers: { "X-Request-ID": "request_bob" } }),
    );
    expect(await bobResponse.json()).toEqual(users[bobToken]);
    releaseAlice(aliceToken);
    const completedAliceResponse = await aliceResponse;

    expect(await completedAliceResponse.json()).toEqual(users[aliceToken]);
    expect(completedAliceResponse.headers.get("X-Request-ID")).toBe("request_alice");
    expect(bobResponse.headers.get("X-Request-ID")).toBe("request_bob");
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(
      fetcher.mock.calls.map(([input]) => {
        const request = input as Request;
        return [request.headers.get("Authorization"), request.headers.get("X-Request-ID")];
      }),
    ).toEqual([
      [`Bearer ${bobToken}`, "request_bob"],
      [`Bearer ${aliceToken}`, "request_alice"],
    ]);
  });

  it("serves the actual /api/me route using the event's Clerk session", async () => {
    const session = clerkSession();
    const user = { id: randomUUID(), clerkSubject: session.userId };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(Response.json(user));
    vi.stubGlobal("fetch", fetcher);
    vi.stubGlobal("defineEventHandler", defineEventHandler);
    vi.doMock("#imports", () => ({ useRuntimeConfig: () => config }));
    const { default: me } = await import("../server/api/me.get");
    const app = createApp()
      .use(requestId)
      .use(
        defineEventHandler((event) => {
          Object.assign(event.context, { auth: vi.fn(() => session) });
        }),
      )
      .use("/api/me", me);
    const response = await toWebHandler(app)(new Request(`${config.appOrigin}/api/me`));

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual(user);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(fetcher).toHaveBeenCalledOnce();
    expect((fetcher.mock.calls[0]![0] as Request).url).toBe(`${config.userApiUrl}/me`);
  });
});
