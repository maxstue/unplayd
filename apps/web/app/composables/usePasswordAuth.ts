import { computed, onScopeDispose, ref, watch, type Ref } from "vue";
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
  const step = ref<Step>("credentials");
  const email = ref("");
  const password = ref("");
  const code = ref("");
  const error = ref("");
  const notice = ref("");
  const busy = ref(false);
  const resendIn = ref(0);
  const factor = ref<Factor>();
  const factors = ref<Factor[]>([]);
  let firstFactor = false;
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
    if (!sdk.signIn.value) throw new Error("Authentication not ready");
    return sdk.signIn.value;
  }
  function signUp() {
    if (!sdk.signUp.value) throw new Error("Authentication not ready");
    return sdk.signUp.value;
  }
  function startCooldown() {
    clearInterval(cooldown);
    resendIn.value = 30;
    cooldown = setInterval(() => {
      resendIn.value--;
      if (resendIn.value <= 0) clearInterval(cooldown);
    }, 1000);
  }
  async function run(action: () => Promise<void>) {
    if (!ready.value || disposed) return;
    busy.value = true;
    error.value = "";
    notice.value = "";
    try {
      await action();
    } catch (cause) {
      if (!disposed) error.value = authErrorMessage(cause);
    } finally {
      busy.value = false;
    }
  }
  function block() {
    step.value = "blocked";
    error.value =
      "Für dieses Konto ist ein weiterer Sicherheitsschritt erforderlich, der hier noch nicht unterstützt wird. Die Anmeldung wurde nicht abgeschlossen.";
  }
  async function finish(result: { status: string | null; createdSessionId: string | null }) {
    if (disposed) return;
    if (result.status !== "complete" || !result.createdSessionId || !sdk.setActive.value) {
      block();
      return;
    }
    password.value = "";
    code.value = "";
    await sdk.setActive.value({
      session: result.createdSessionId,
      navigate: async ({ session }) => {
        if (disposed) return;
        if (session?.currentTask || session?.status !== "active") {
          block();
          return;
        }
        await onComplete();
      },
    });
  }
  async function prepareFactor(selected: Factor) {
    factor.value = selected;
    code.value = "";
    if (selected.strategy === "email_code") {
      if (firstFactor)
        await signIn().prepareFirstFactor({
          strategy: "email_code",
          emailAddressId: selected.emailAddressId,
        });
      else
        await signIn().prepareSecondFactor({
          strategy: "email_code",
          emailAddressId: selected.emailAddressId,
        });
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
    if (disposed) return;
    if (result.status === "complete") {
      await finish(result);
      return;
    }
    if (result.status === "needs_new_password") {
      step.value = "renew";
      return;
    }
    firstFactor = result.status === "needs_first_factor";
    const available = firstFactor ? result.supportedFirstFactors : result.supportedSecondFactors;
    const strategies = firstFactor
      ? ["email_code"]
      : ["totp", "email_code", "phone_code", "backup_code", "passkey"];
    if (
      !["needs_first_factor", "needs_second_factor", "needs_client_trust"].includes(
        result.status ?? "",
      )
    ) {
      block();
      return;
    }
    factors.value = strategies.flatMap((strategy) =>
      (available ?? []).filter((item) => item.strategy === strategy),
    ) as Factor[];
    const selected = factors.value[0];
    if (!selected) {
      block();
      return;
    }
    step.value = "second-factor";
    await prepareFactor(selected);
  }
  async function submitCredentials() {
    if (mode === "sign-up") {
      const result = await signUp().create({
        emailAddress: email.value.trim(),
        password: password.value,
      });
      password.value = "";
      if (result.status === "complete") {
        await finish(result);
        return;
      }
      if (result.missingFields.length || !result.unverifiedFields.includes("email_address")) {
        block();
        return;
      }
      step.value = "verify-email";
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
    const strategy = factor.value?.strategy;
    if (!strategy) {
      block();
      return;
    }
    let result: SignIn;
    if (strategy === "passkey") result = await signIn().authenticateWithPasskey();
    else if (firstFactor && strategy === "email_code")
      result = await signIn().attemptFirstFactor({ strategy, code: code.value.trim() });
    else if (["totp", "backup_code", "email_code", "phone_code"].includes(strategy))
      result = await signIn().attemptSecondFactor({
        strategy: strategy as "totp" | "backup_code" | "email_code" | "phone_code",
        code: code.value.trim(),
      });
    else {
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
    step.value = "reset";
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
    if (result.status !== "needs_new_password") password.value = "";
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
    if (resendIn.value > 0 || !canResend.value) return;
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
      } else if (factor.value) await prepareFactor(factor.value);
      notice.value = "Ein neuer Code wurde angefordert.";
    });
  }
  async function selectFactor(strategy: string) {
    await run(async () => {
      const selected = factors.value.find((item) => item.strategy === strategy);
      if (selected) await prepareFactor(selected);
    });
  }
  function restart(recovery = false) {
    if (busy.value) return;
    step.value = recovery ? "recover" : "credentials";
    password.value = "";
    code.value = "";
    error.value = "";
    notice.value = "";
    factor.value = undefined;
    factors.value = [];
    clearInterval(cooldown);
    resendIn.value = 0;
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
        step.value = "verify-email";
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
