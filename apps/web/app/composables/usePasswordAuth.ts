import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from "vue";
import type { useSignIn, useSignUp } from "@clerk/nuxt/composables";
import { authErrorMessage } from "#shared/auth";

type SignIn = NonNullable<ReturnType<typeof useSignIn>["signIn"]["value"]>;
type SignUp = NonNullable<ReturnType<typeof useSignUp>["signUp"]["value"]>;
type SetActive = NonNullable<ReturnType<typeof useSignIn>["setActive"]["value"]>;
type Factor = NonNullable<SignIn["supportedSecondFactors"]>[number];
type Step =
  | "credentials"
  | "verify-email"
  | "second-factor"
  | "recover"
  | "reset"
  | "renew"
  | "blocked";
type EmailFactor = Extract<Factor, { strategy: "email_code" }>;
type Challenge =
  | { step: "second-factor"; level: "first"; factor: EmailFactor; factors: EmailFactor[] }
  | { step: "second-factor"; level: "second"; factor: Factor; factors: Factor[] };

type AuthState = { step: Exclude<Step, "second-factor"> } | Challenge;
export interface PasswordAuthSdk {
  signIn: Readonly<Ref<SignIn | undefined>>;
  signUp: Readonly<Ref<SignUp | undefined>>;
  setActive: Readonly<Ref<SetActive | undefined>>;
  isLoaded: Readonly<Ref<boolean>>;
}

export function usePasswordAuth(
  mode: "sign-in" | "sign-up",
  sdk: PasswordAuthSdk,
  onComplete: () => Promise<void>,
) {
  const state = shallowRef<AuthState>({ step: "credentials" });
  const step = computed(() => state.value.step);
  const challenge = computed(() =>
    state.value.step === "second-factor" ? state.value : undefined,
  );
  const factor = computed(() => challenge.value?.factor);
  const factors = computed(() => challenge.value?.factors ?? []);
  const email = ref("");
  const password = ref("");
  const code = ref("");
  const error = ref("");
  const notice = ref("");
  const busy = ref(false);
  const resendIn = ref(0);
  let disposed = false;
  let cooldown: ReturnType<typeof setInterval> | undefined;
  const ready = computed(() => sdk.isLoaded.value && !busy.value);
  const codeDelivery = computed(() => (factor.value?.strategy === "phone_code" ? "SMS" : "E-Mail"));
  const needsCode = computed(() => factor.value?.strategy !== "passkey");
  const canResend = computed(
    () =>
      step.value === "verify-email" ||
      step.value === "reset" ||
      (step.value === "second-factor" &&
        ["email_code", "phone_code"].includes(factor.value?.strategy ?? "")),
  );

  function signIn() {
    if (!sdk.signIn.value) {
      throw new Error("Authentication not ready");
    }
    return sdk.signIn.value;
  }
  function signUp() {
    if (!sdk.signUp.value) {
      throw new Error("Authentication not ready");
    }
    return sdk.signUp.value;
  }
  function enter(next: AuthState) {
    if (disposed) {
      return;
    }
    clearInterval(cooldown);
    resendIn.value = 0;
    code.value = "";
    state.value = next;
  }
  function startCooldown() {
    if (disposed) {
      return;
    }
    clearInterval(cooldown);
    resendIn.value = 30;
    cooldown = setInterval(() => {
      resendIn.value--;
      if (resendIn.value <= 0) {
        clearInterval(cooldown);
      }
    }, 1000);
  }
  async function run(action: () => Promise<void>) {
    if (!ready.value || disposed) {
      return;
    }
    busy.value = true;
    error.value = "";
    notice.value = "";
    try {
      await action();
    } catch (cause) {
      if (!disposed) {
        error.value = authErrorMessage(cause);
      }
    } finally {
      busy.value = false;
    }
  }
  function block() {
    enter({ step: "blocked" });
    error.value =
      "Für dieses Konto ist ein weiterer Sicherheitsschritt erforderlich, der hier noch nicht unterstützt wird. Die Anmeldung wurde nicht abgeschlossen.";
  }
  async function finish(result: { status: string | null; createdSessionId: string | null }) {
    if (disposed) {
      return;
    }
    if (result.status !== "complete" || !result.createdSessionId || !sdk.setActive.value) {
      block();
      return;
    }
    password.value = "";
    code.value = "";
    await sdk.setActive.value({
      session: result.createdSessionId,
      navigate: async ({ session }) => {
        if (disposed) {
          return;
        }
        if (session?.currentTask || session?.status !== "active") {
          block();
          return;
        }
        await onComplete();
      },
    });
  }
  async function prepareFactor(next: Challenge) {
    enter(next);
    const selected = next.factor;
    if (selected.strategy === "email_code") {
      if (next.level === "first") {
        await signIn().prepareFirstFactor({
          strategy: "email_code",
          emailAddressId: selected.emailAddressId,
        });
      } else {
        await signIn().prepareSecondFactor({
          strategy: "email_code",
          emailAddressId: selected.emailAddressId,
        });
      }
      startCooldown();
    } else if (selected.strategy === "phone_code") {
      await signIn().prepareSecondFactor({
        strategy: "phone_code",
        phoneNumberId: selected.phoneNumberId,
      });
      startCooldown();
    }
  }
  async function advanceSignIn(result: SignIn) {
    if (disposed) {
      return;
    }
    if (result.status === "complete") {
      await finish(result);
      return;
    }
    if (result.status === "needs_new_password") {
      enter({ step: "renew" });
      return;
    }
    if (result.status === "needs_first_factor") {
      const available = (result.supportedFirstFactors ?? []).filter(
        (item): item is EmailFactor => item.strategy === "email_code",
      );
      const selected = available[0];
      if (selected) {
        await prepareFactor({
          step: "second-factor",
          level: "first",
          factor: selected,
          factors: available,
        });
      } else {
        block();
      }
      return;
    }
    if (result.status !== "needs_second_factor" && result.status !== "needs_client_trust") {
      block();
      return;
    }
    const available = ["totp", "email_code", "phone_code", "backup_code", "passkey"].flatMap(
      (strategy) =>
        (result.supportedSecondFactors ?? []).filter((item) => item.strategy === strategy),
    );
    const selected = available[0];
    if (selected) {
      await prepareFactor({
        step: "second-factor",
        level: "second",
        factor: selected,
        factors: available,
      });
    } else {
      block();
    }
  }
  async function submitCredentials() {
    if (mode === "sign-up") {
      const result = await signUp().create({
        emailAddress: email.value.trim(),
        password: password.value,
      });
      if (disposed) {
        return;
      }
      password.value = "";
      if (result.status === "complete") {
        await finish(result);
        return;
      }
      if (result.missingFields.length || !result.unverifiedFields.includes("email_address")) {
        block();
        return;
      }
      enter({ step: "verify-email" });
      await signUp().prepareEmailAddressVerification({ strategy: "email_code" });
      startCooldown();
      return;
    }
    const result = await signIn().create({
      strategy: "password",
      identifier: email.value.trim(),
      password: password.value,
    });
    password.value = "";
    await advanceSignIn(result);
  }
  async function submitFactor() {
    const current = challenge.value;
    const strategy = current?.factor.strategy;
    if (!current || !strategy) {
      block();
      return;
    }
    let result: SignIn;
    if (strategy === "passkey") {
      result = await signIn().authenticateWithPasskey();
    } else if (current.level === "first" && strategy === "email_code") {
      result = await signIn().attemptFirstFactor({ strategy, code: code.value.trim() });
    } else if (["totp", "backup_code", "email_code", "phone_code"].includes(strategy)) {
      result = await signIn().attemptSecondFactor({
        strategy: strategy as "totp" | "backup_code" | "email_code" | "phone_code",
        code: code.value.trim(),
      });
    } else {
      block();
      return;
    }
    code.value = "";
    await advanceSignIn(result);
  }
  async function requestReset() {
    await signIn().create({
      strategy: "reset_password_email_code",
      identifier: email.value.trim(),
    });
    if (disposed) {
      return;
    }
    enter({ step: "reset" });
    password.value = "";
    code.value = "";
    startCooldown();
  }
  async function submitReset() {
    const result = await signIn().attemptFirstFactor({
      strategy: "reset_password_email_code",
      code: code.value.trim(),
      password: password.value,
    });
    if (result.status !== "needs_new_password") {
      password.value = "";
    }
    code.value = "";
    await advanceSignIn(result);
  }
  async function submitRenewal() {
    const result = await signIn().resetPassword({
      password: password.value,
      signOutOfOtherSessions: true,
    });
    password.value = "";
    await advanceSignIn(result);
  }
  async function submit() {
    await run(async () => {
      switch (step.value) {
        case "credentials":
          return submitCredentials();
        case "verify-email":
          return finish(
            await signUp().attemptEmailAddressVerification({ code: code.value.trim() }),
          );
        case "second-factor":
          return submitFactor();
        case "recover":
          return requestReset();
        case "reset":
          return submitReset();
        case "renew":
          return submitRenewal();
      }
    });
  }
  async function resend() {
    if (resendIn.value > 0 || !canResend.value) {
      return;
    }
    await run(async () => {
      if (step.value === "verify-email") {
        await signUp().prepareEmailAddressVerification({ strategy: "email_code" });
        startCooldown();
      } else if (step.value === "reset") {
        await signIn().create({
          strategy: "reset_password_email_code",
          identifier: email.value.trim(),
        });
        startCooldown();
      } else if (challenge.value) {
        await prepareFactor(challenge.value);
      }
      if (!disposed) {
        notice.value = "Ein neuer Code wurde angefordert.";
      }
    });
  }
  async function selectFactor(strategy: string) {
    await run(async () => {
      const current = challenge.value;
      if (!current) {
        return;
      }
      const selected = current.factors.find((item) => item.strategy === strategy);
      if (selected) {
        await prepareFactor({ ...current, factor: selected } as Challenge);
      }
    });
  }
  function restart(recovery = false) {
    if (busy.value || disposed) {
      return;
    }
    enter({ step: recovery ? "recover" : "credentials" });
    password.value = "";
    code.value = "";
    error.value = "";
    notice.value = "";
  }
  // Restore only an unfinished email verification, never a password or code.
  const stopRestore = watch(
    sdk.isLoaded,
    (loaded) => {
      const pending = sdk.signUp.value;
      if (
        loaded &&
        mode === "sign-up" &&
        pending?.status === "missing_requirements" &&
        pending.hasPassword &&
        pending.unverifiedFields.includes("email_address") &&
        !pending.missingFields.length
      ) {
        email.value = pending.emailAddress ?? "";
        enter({ step: "verify-email" });
      }
    },
    { immediate: true },
  );
  onScopeDispose(() => {
    disposed = true;
    clearInterval(cooldown);
    stopRestore();
    password.value = "";
    code.value = "";
  });
  return {
    step,
    email,
    password,
    code,
    error,
    notice,
    busy,
    ready,
    resendIn,
    factor,
    factors,
    codeDelivery,
    needsCode,
    canResend,
    submit,
    resend,
    selectFactor,
    restart,
  };
}
