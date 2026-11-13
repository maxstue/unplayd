import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { effectScope, ref } from "vue";
import { randomUUID } from "node:crypto";
import { usePasswordAuth, type PasswordAuthSdk } from "../app/composables/usePasswordAuth";
import { authErrorMessage } from "../shared/auth";

type SignIn = NonNullable<PasswordAuthSdk["signIn"]["value"]>;
type SignUp = NonNullable<PasswordAuthSdk["signUp"]["value"]>;
type SetActive = NonNullable<PasswordAuthSdk["setActive"]["value"]>;
const scopes: ReturnType<typeof effectScope>[] = [];
afterEach(() => {
  scopes.splice(0).forEach((scope) => scope.stop());
  vi.useRealTimers();
});
function setup(mode: "sign-in" | "sign-up" = "sign-in", pendingTask = false) {
  const incoming = {
    status: null as string | null,
    createdSessionId: null as string | null,
    supportedFirstFactors: [] as object[],
    supportedSecondFactors: [] as object[],
    create: vi.fn(),
    prepareFirstFactor: vi.fn(),
    prepareSecondFactor: vi.fn(),
    attemptFirstFactor: vi.fn(),
    attemptSecondFactor: vi.fn(),
    resetPassword: vi.fn(),
    authenticateWithPasskey: vi.fn(),
  };
  const outgoing = {
    status: null as string | null,
    createdSessionId: null as string | null,
    missingFields: [] as string[],
    unverifiedFields: ["email_address"],
    hasPassword: false,
    emailAddress: "",
    create: vi.fn(),
    prepareEmailAddressVerification: vi.fn(),
    attemptEmailAddressVerification: vi.fn(),
  };
  const complete = vi.fn(async () => {});
  const setActive = vi.fn<SetActive>(async (params) => {
    await params.navigate?.({
      session: {
        status: pendingTask ? "pending" : "active",
        currentTask: pendingTask ? { key: "choose-organization" } : null,
      },
    } as never);
  });
  const sdk = {
    signIn: ref(incoming as unknown as SignIn),
    signUp: ref(outgoing as unknown as SignUp),
    isLoaded: ref(true),
    setActive: ref(setActive),
  } satisfies PasswordAuthSdk;
  const scope = effectScope();
  scopes.push(scope);
  const flow = scope.run(() => usePasswordAuth(mode, sdk, complete))!;
  incoming.create.mockResolvedValue(incoming);
  outgoing.create.mockImplementation(async () => {
    outgoing.status = "missing_requirements";
    outgoing.hasPassword = true;
    return outgoing;
  });
  return { flow, incoming, outgoing, setActive, complete, scope, sdk };
}
function signedIn(incoming: ReturnType<typeof setup>["incoming"]) {
  incoming.status = "complete";
  incoming.createdSessionId = randomUUID();
  return incoming;
}

describe("custom password authentication", () => {
  it("waits for the SDK and prevents duplicate requests while a submission is in flight", async () => {
    const { flow, incoming, sdk } = setup();
    sdk.isLoaded.value = false;
    await flow.submit();
    expect(incoming.create).not.toHaveBeenCalled();
    sdk.isLoaded.value = true;
    let resolve!: (value: unknown) => void;
    incoming.create.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const first = flow.submit();
    await flow.submit();
    expect(incoming.create).toHaveBeenCalledTimes(1);
    resolve(signedIn(incoming));
    await first;
    expect(flow.busy.value).toBe(false);
  });

  it("requires email verification before activating a newly registered account", async () => {
    const { flow, outgoing, setActive, complete } = setup("sign-up");
    const value = randomUUID();
    flow.email.value = " person@example.com ";
    flow.password.value = value;
    await flow.submit();
    expect(outgoing.create).toHaveBeenCalledWith({
      emailAddress: "person@example.com",
      password: value,
    });
    expect(flow.password.value).toBe("");
    expect(flow.step.value).toBe("verify-email");
    expect(setActive).not.toHaveBeenCalled();
    outgoing.status = "complete";
    outgoing.createdSessionId = randomUUID();
    outgoing.attemptEmailAddressVerification.mockResolvedValue(outgoing);
    flow.code.value = String(Math.floor(Math.random() * 900000) + 100000);
    await flow.submit();
    expect(setActive).toHaveBeenCalledWith(
      expect.objectContaining({ session: outgoing.createdSessionId }),
    );
    expect(complete).toHaveBeenCalledTimes(1);
    expect(flow.code.value).toBe("");
  });

  it("can retry delivery after registration succeeds but sending the code fails", async () => {
    vi.useFakeTimers();
    const { flow, outgoing } = setup("sign-up");
    outgoing.prepareEmailAddressVerification
      .mockRejectedValueOnce(new Error(randomUUID()))
      .mockResolvedValue(undefined);
    await flow.submit();
    expect(flow.step.value).toBe("verify-email");
    expect(flow.canResend.value).toBe(true);
    await flow.resend();
    expect(flow.resendIn.value).toBe(30);
    await flow.resend();
    expect(outgoing.prepareEmailAddressVerification).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(30000);
    await flow.resend();
    expect(outgoing.prepareEmailAddressVerification).toHaveBeenCalledTimes(3);
    expect(outgoing.create).toHaveBeenCalledTimes(1);
  });

  it("restores an unfinished verification without resending or storing a password", async () => {
    const { flow, outgoing, sdk } = setup("sign-up");
    sdk.isLoaded.value = false;
    Object.assign(outgoing, {
      status: "missing_requirements",
      hasPassword: true,
      emailAddress: "person@example.com",
    });
    await Promise.resolve();
    sdk.isLoaded.value = true;
    await Promise.resolve();
    expect(flow.step.value).toBe("verify-email");
    expect(flow.email.value).toBe("person@example.com");
    expect(flow.password.value).toBe("");
    expect(outgoing.prepareEmailAddressVerification).not.toHaveBeenCalled();
  });

  it("activates only a complete password sign-in and clears the password", async () => {
    const { flow, incoming, setActive, complete } = setup();
    signedIn(incoming);
    const value = randomUUID();
    flow.password.value = value;
    await flow.submit();
    expect(setActive).toHaveBeenCalledTimes(1);
    expect(complete).toHaveBeenCalledTimes(1);
    expect(flow.password.value).toBe("");
  });

  it("completes Device Trust with an email second factor and permits retry after a wrong code", async () => {
    const { flow, incoming, setActive } = setup();
    Object.assign(incoming, {
      status: "needs_client_trust",
      supportedSecondFactors: [
        {
          strategy: "email_code",
          emailAddressId: randomUUID(),
          safeIdentifier: "p***@example.com",
        },
      ],
    });
    await flow.submit();
    expect(flow.step.value).toBe("second-factor");
    expect(incoming.prepareSecondFactor).toHaveBeenCalledWith(
      expect.objectContaining({ strategy: "email_code" }),
    );
    incoming.attemptSecondFactor
      .mockRejectedValueOnce({ errors: [{ code: "form_code_incorrect" }] })
      .mockImplementationOnce(async () => signedIn(incoming));
    flow.code.value = "000000";
    await flow.submit();
    expect(flow.error.value).toContain("Code");
    expect(setActive).not.toHaveBeenCalled();
    await flow.submit();
    expect(setActive).toHaveBeenCalledTimes(1);
  });

  it("supports switching from an authenticator to a recovery code without sending an email", async () => {
    const { flow, incoming } = setup();
    Object.assign(incoming, {
      status: "needs_second_factor",
      supportedSecondFactors: [{ strategy: "totp" }, { strategy: "backup_code" }],
    });
    await flow.submit();
    expect(flow.factor.value?.strategy).toBe("totp");
    expect(flow.canResend.value).toBe(false);
    await flow.selectFactor("backup_code");
    flow.code.value = randomUUID();
    incoming.attemptSecondFactor.mockImplementation(async () => signedIn(incoming));
    await flow.submit();
    expect(incoming.attemptSecondFactor).toHaveBeenCalledWith(
      expect.objectContaining({ strategy: "backup_code" }),
    );
    expect(incoming.prepareSecondFactor).not.toHaveBeenCalled();
  });

  it("uses separate first-factor and second-factor challenges during the same sign-in", async () => {
    const { flow, incoming, setActive } = setup();
    const emailAddressId = randomUUID();
    Object.assign(incoming, {
      status: "needs_first_factor",
      supportedFirstFactors: [{ strategy: "password" }, { strategy: "email_code", emailAddressId }],
      supportedSecondFactors: [{ strategy: "totp" }],
    });
    await flow.submit();
    expect(flow.factors.value.map((item) => item.strategy)).toEqual(["email_code"]);
    expect(incoming.prepareFirstFactor).toHaveBeenCalledWith({
      strategy: "email_code",
      emailAddressId,
    });
    await flow.selectFactor("totp");
    expect(flow.factor.value?.strategy).toBe("email_code");

    incoming.attemptFirstFactor.mockImplementation(async () => {
      incoming.status = "needs_second_factor";
      return incoming;
    });
    flow.code.value = "123456";
    await flow.submit();
    expect(incoming.attemptFirstFactor).toHaveBeenCalledWith({
      strategy: "email_code",
      code: "123456",
    });
    expect(flow.factor.value?.strategy).toBe("totp");
    expect(flow.canResend.value).toBe(false);
    expect(flow.code.value).toBe("");
    expect(flow.resendIn.value).toBe(0);

    incoming.attemptSecondFactor.mockImplementation(async () => signedIn(incoming));
    flow.code.value = "654321";
    await flow.submit();
    expect(incoming.attemptSecondFactor).toHaveBeenCalledWith({ strategy: "totp", code: "654321" });
    expect(setActive).toHaveBeenCalledTimes(1);
  });

  it("drops the previous verification method when the flow is blocked or restarted", async () => {
    vi.useFakeTimers();
    const { flow, incoming } = setup();
    Object.assign(incoming, {
      status: "needs_second_factor",
      supportedSecondFactors: [{ strategy: "email_code", emailAddressId: randomUUID() }],
    });
    await flow.submit();
    expect(flow.resendIn.value).toBe(30);
    incoming.attemptSecondFactor.mockImplementation(async () => {
      incoming.status = "needs_protect_check";
      return incoming;
    });
    await flow.submit();
    expect(flow.step.value).toBe("blocked");
    expect(flow.factor.value).toBeUndefined();
    expect(flow.factors.value).toEqual([]);
    expect(flow.resendIn.value).toBe(0);
    expect(vi.getTimerCount()).toBe(0);
    flow.restart();
    expect(flow.step.value).toBe("credentials");
    expect(flow.error.value).toBe("");
  });

  it("sends an SMS only for a supported phone factor", async () => {
    const { flow, incoming } = setup();
    const phoneNumberId = randomUUID();
    Object.assign(incoming, {
      status: "needs_second_factor",
      supportedSecondFactors: [{ strategy: "phone_code", phoneNumberId, safeIdentifier: "***123" }],
    });
    await flow.submit();
    expect(incoming.prepareSecondFactor).toHaveBeenCalledWith({
      strategy: "phone_code",
      phoneNumberId,
    });
    expect(flow.codeDelivery.value).toBe("SMS");
    expect(flow.canResend.value).toBe(true);
  });

  it("can confirm a supported passkey without asking for a code", async () => {
    const { flow, incoming, setActive } = setup();
    Object.assign(incoming, {
      status: "needs_second_factor",
      supportedSecondFactors: [{ strategy: "passkey" }],
    });
    await flow.submit();
    expect(flow.needsCode.value).toBe(false);
    incoming.authenticateWithPasskey.mockImplementation(async () => signedIn(incoming));
    await flow.submit();
    expect(incoming.authenticateWithPasskey).toHaveBeenCalledTimes(1);
    expect(setActive).toHaveBeenCalledTimes(1);
  });

  it("resets a password with its verification code before activating the session", async () => {
    const { flow, incoming, setActive } = setup();
    flow.email.value = "person@example.com";
    flow.restart(true);
    await flow.submit();
    expect(incoming.create).toHaveBeenCalledWith({
      strategy: "reset_password_email_code",
      identifier: "person@example.com",
    });
    expect(flow.step.value).toBe("reset");
    expect(setActive).not.toHaveBeenCalled();
    const value = randomUUID();
    const code = String(Math.floor(Math.random() * 900000) + 100000);
    flow.password.value = value;
    flow.code.value = code;
    incoming.attemptFirstFactor.mockImplementation(async () => signedIn(incoming));
    await flow.submit();
    expect(incoming.attemptFirstFactor).toHaveBeenCalledWith({
      strategy: "reset_password_email_code",
      code,
      password: value,
    });
    expect(setActive).toHaveBeenCalledTimes(1);
  });

  it("handles required password renewal and revokes other sessions", async () => {
    const { flow, incoming } = setup();
    incoming.status = "needs_new_password";
    await flow.submit();
    expect(flow.step.value).toBe("renew");
    const value = randomUUID();
    flow.password.value = value;
    incoming.resetPassword.mockImplementation(async () => signedIn(incoming));
    await flow.submit();
    expect(incoming.resetPassword).toHaveBeenCalledWith({
      password: value,
      signOutOfOtherSessions: true,
    });
  });

  it("never treats unsupported factors, missing session ids or pending tasks as completed login", async () => {
    const unsupported = setup();
    unsupported.incoming.status = "needs_protect_check";
    await unsupported.flow.submit();
    expect(unsupported.flow.step.value).toBe("blocked");
    expect(unsupported.setActive).not.toHaveBeenCalled();
    const missing = setup();
    missing.incoming.status = "complete";
    await missing.flow.submit();
    expect(missing.setActive).not.toHaveBeenCalled();
    const pending = setup("sign-in", true);
    signedIn(pending.incoming);
    await pending.flow.submit();
    expect(pending.flow.step.value).toBe("blocked");
    expect(pending.complete).not.toHaveBeenCalled();
  });

  it("does not activate a late response after leaving the page", async () => {
    const { flow, incoming, setActive, scope } = setup();
    let resolve!: (value: unknown) => void;
    incoming.create.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    flow.password.value = randomUUID();
    const request = flow.submit();
    scope.stop();
    resolve(signedIn(incoming));
    await request;
    expect(setActive).not.toHaveBeenCalled();
    expect(flow.password.value).toBe("");
  });

  it("does not send a verification email when registration completes after leaving the page", async () => {
    const { flow, outgoing, scope } = setup("sign-up");
    let resolve!: (value: unknown) => void;
    outgoing.create.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const request = flow.submit();
    scope.stop();
    outgoing.status = "missing_requirements";
    resolve(outgoing);
    await request;
    expect(outgoing.prepareEmailAddressVerification).not.toHaveBeenCalled();
    expect(flow.step.value).toBe("credentials");
  });

  it("does not restart the resend timer when a pending delivery finishes after leaving the page", async () => {
    vi.useFakeTimers();
    const { flow, outgoing, scope } = setup("sign-up");
    let resolve!: () => void;
    outgoing.prepareEmailAddressVerification.mockReturnValue(
      new Promise<void>((done) => {
        resolve = done;
      }),
    );
    const request = flow.submit();
    await Promise.resolve();
    expect(outgoing.prepareEmailAddressVerification).toHaveBeenCalledTimes(1);
    scope.stop();
    resolve();
    await request;
    expect(vi.getTimerCount()).toBe(0);
    expect(flow.resendIn.value).toBe(0);
  });

  it("stops an active resend timer and clears secrets when leaving the page", async () => {
    vi.useFakeTimers();
    const { flow, scope } = setup("sign-up");
    await flow.submit();
    flow.code.value = "123456";
    flow.password.value = randomUUID();
    expect(vi.getTimerCount()).toBe(1);
    scope.stop();
    expect(vi.getTimerCount()).toBe(0);
    expect(flow.code.value).toBe("");
    expect(flow.password.value).toBe("");
  });
});

describe("public auth errors", () => {
  it("does not reflect provider messages, private values or unknown error codes", () => {
    const privateValue = randomUUID();
    for (const code of ["form_code_incorrect", "too_many_requests", "unknown", "__proto__"]) {
      const message = authErrorMessage({
        errors: [{ code, message: privateValue, longMessage: privateValue }],
      });
      expect(message).not.toContain(privateValue);
      expect(typeof message).toBe("string");
    }
    expect(authErrorMessage(new Error(privateValue))).not.toContain(privateValue);
    expect(authErrorMessage({ errors: [{ code: "form_password_incorrect" }] })).toBe(
      authErrorMessage({ errors: [{ code: "form_identifier_not_found" }] }),
    );
  });
});
