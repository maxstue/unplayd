<script setup lang="ts">
import { useSignIn, useSignUp, useAuth } from "@clerk/nuxt/composables";
const props = defineProps<{ mode: "sign-in" | "sign-up" }>();
const incoming = useSignIn();
const outgoing = useSignUp();
const { isSignedIn } = useAuth();
const {
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
} = usePasswordAuth(
  props.mode,
  {
    signIn: incoming.signIn,
    signUp: outgoing.signUp,
    setActive: incoming.setActive,
    isLoaded: computed(() => incoming.isLoaded.value && outgoing.isLoaded.value),
  },
  async () => {
    await navigateTo("/dashboard");
  },
);
watch(
  isSignedIn,
  (signedIn) => {
    if (signedIn) {
      void navigateTo("/dashboard");
    }
  },
  { immediate: true },
);
const heading = computed(() => {
  if (step.value === "verify-email") {
    return "Bestätige deine E-Mail.";
  }
  if (step.value === "second-factor") {
    return "Ein letzter Sicherheitscheck.";
  }
  if (["recover", "reset", "renew"].includes(step.value)) {
    return "Ein neues Passwort.";
  }
  if (step.value === "blocked") {
    return "Noch ein Schritt fehlt.";
  }
  return props.mode === "sign-up" ? "Dein Backlog. Dein Platz." : "Willkommen zurück.";
});
function secondFactorDescription() {
  if (factor.value?.strategy === "totp") {
    return "Gib den aktuellen Code aus deiner Authenticator-App ein.";
  }
  if (factor.value?.strategy === "backup_code") {
    return "Gib einen deiner unbenutzten Wiederherstellungscodes ein.";
  }
  if (factor.value?.strategy === "passkey") {
    return "Bestätige die Anmeldung mit deinem Passkey.";
  }
  const recipient =
    factor.value && "safeIdentifier" in factor.value ? factor.value.safeIdentifier : "";
  const recipientSuffix = recipient ? ` (${recipient})` : "";
  return `Gib den Code aus deiner ${codeDelivery.value} ein${recipientSuffix}.`;
}
const description = computed(() => {
  if (step.value === "verify-email") {
    return `Gib den Code ein, den wir an ${email.value} geschickt haben.`;
  }
  if (step.value === "second-factor") {
    return secondFactorDescription();
  }
  if (step.value === "recover") {
    return "Wir schicken dir einen Code, mit dem du dein Passwort zurücksetzen kannst.";
  }
  if (step.value === "reset") {
    return `Gib den Code für ${email.value} ein und wähle dein neues Passwort.`;
  }
  if (step.value === "renew") {
    return "Wähle ein neues Passwort, um die Anmeldung abzuschließen.";
  }
  if (step.value === "blocked") {
    return "Dein Konto benötigt einen zusätzlichen Sicherheitsschritt.";
  }
  return props.mode === "sign-up"
    ? "Erstelle dein Konto und bring Ordnung in deine Spiele."
    : "Deine Spiele warten schon auf dich.";
});
const buttonLabel = computed(() => {
  if (busy.value) {
    return "Einen Moment …";
  }
  if (step.value === "verify-email") {
    return "E-Mail bestätigen";
  }
  if (step.value === "second-factor") {
    return needsCode.value ? "Anmeldung bestätigen" : "Mit Passkey bestätigen";
  }
  if (step.value === "recover") {
    return "Code anfordern";
  }
  if (["reset", "renew"].includes(step.value)) {
    return "Passwort speichern";
  }
  return props.mode === "sign-up" ? "Konto erstellen" : "Anmelden";
});
const factorLabels: Record<string, string> = {
  totp: "Authenticator-App",
  email_code: "E-Mail-Code",
  phone_code: "SMS-Code",
  backup_code: "Wiederherstellungscode",
  passkey: "Passkey",
};
const title = ref<HTMLElement>();
watch(step, async () => {
  await nextTick();
  title.value?.focus();
});
</script>

<template>
  <section class="auth-view" aria-labelledby="auth-title">
    <NuxtLink to="/" class="auth-back">Zurück zu den Spielen</NuxtLink>
    <div class="auth-heading">
      <h1 id="auth-title" ref="title" tabindex="-1">{{ heading }}</h1>
      <p>{{ description }}</p>
    </div>
    <output v-if="!incoming.isLoaded.value || !outgoing.isLoaded.value" class="field-hint">
      Anmeldung wird vorbereitet …
    </output>
    <form v-if="step !== 'blocked'" class="auth-form" :aria-busy="busy" @submit.prevent="submit()">
      <div v-if="step === 'credentials' || step === 'recover'" class="auth-field">
        <label for="auth-email">E-Mail-Adresse</label
        ><input
          id="auth-email"
          v-model="email"
          type="email"
          name="email"
          autocomplete="email"
          inputmode="email"
          autocapitalize="none"
          spellcheck="false"
          :disabled="busy"
          required
        />
      </div>
      <PasswordField
        v-if="step === 'credentials'"
        id="auth-password"
        v-model="password"
        label="Passwort"
        :is-new="mode === 'sign-up'"
        :disabled="busy"
      />
      <button
        v-if="step === 'credentials' && mode === 'sign-in'"
        type="button"
        class="text-button auth-forgot"
        :disabled="!ready"
        @click="restart(true)"
      >
        Passwort vergessen?
      </button>
      <div v-if="step === 'second-factor' && factors.length > 1" class="auth-field">
        <label for="auth-factor">Bestätigungsmethode</label
        ><select
          id="auth-factor"
          :value="factor?.strategy"
          :disabled="busy"
          @change="selectFactor(($event.target as HTMLSelectElement).value)"
        >
          <option v-for="option in factors" :key="option.strategy" :value="option.strategy">
            {{ factorLabels[option.strategy] }}
          </option>
        </select>
      </div>
      <div
        v-if="
          step === 'verify-email' || step === 'reset' || (step === 'second-factor' && needsCode)
        "
        class="auth-field"
      >
        <label for="auth-code">{{
          factor?.strategy === "backup_code" ? "Wiederherstellungscode" : "Bestätigungscode"
        }}</label
        ><input
          id="auth-code"
          v-model="code"
          type="text"
          name="code"
          :inputmode="factor?.strategy === 'backup_code' ? 'text' : 'numeric'"
          :autocomplete="factor?.strategy === 'backup_code' ? 'off' : 'one-time-code'"
          :pattern="factor?.strategy === 'backup_code' ? undefined : '[0-9]{6}'"
          :maxlength="factor?.strategy === 'backup_code' ? 64 : 6"
          :minlength="factor?.strategy === 'backup_code' ? 1 : 6"
          :disabled="busy"
          required
        />
      </div>
      <PasswordField
        v-if="step === 'reset' || step === 'renew'"
        id="auth-renew"
        v-model="password"
        label="Neues Passwort"
        is-new
        :disabled="busy"
      />
      <div
        v-if="mode === 'sign-up' && step === 'credentials'"
        id="clerk-captcha"
        data-cl-theme="dark"
      />
      <p v-if="error" id="auth-error" role="alert" class="auth-error">{{ error }}</p>
      <output v-if="notice" class="field-hint">{{ notice }}</output>
      <button class="auth-submit" type="submit" :disabled="!ready">{{ buttonLabel }}</button>
      <button
        v-if="canResend"
        type="button"
        class="text-button"
        :disabled="!ready || resendIn > 0"
        @click="resend()"
      >
        {{ resendIn > 0 ? `Neuer Code in ${resendIn} s` : "Code erneut senden" }}
      </button>
      <button
        v-if="step !== 'credentials'"
        type="button"
        class="text-button"
        :disabled="busy"
        @click="restart()"
      >
        {{ mode === "sign-up" ? "Andere E-Mail-Adresse verwenden" : "Zurück zur Anmeldung" }}
      </button>
    </form>
    <div v-else>
      <p role="alert" class="auth-error">{{ error }}</p>
      <button type="button" class="secondary" @click="restart()">Zurück</button>
    </div>
    <p v-if="step === 'credentials'" class="auth-switch">
      <template v-if="mode === 'sign-up'"
        >Schon dabei? <NuxtLink to="/sign-in">Anmelden</NuxtLink></template
      ><template v-else
        >Noch kein Konto? <NuxtLink to="/sign-up">Konto erstellen</NuxtLink></template
      >
    </p>
  </section>
</template>
