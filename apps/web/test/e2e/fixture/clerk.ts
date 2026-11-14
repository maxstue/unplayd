import { computed, ref } from "vue";
import { useCookie } from "nuxt/app";
import type * as Clerk from "@clerk/nuxt/composables";

export function useAuth() {
  const user = useCookie<string | null>("fixture-user");
  return {
    userId: computed(() => user.value),
    isSignedIn: computed(() => Boolean(user.value)),
    isLoaded: ref(true),
  };
}

export function useSignIn(): ReturnType<typeof Clerk.useSignIn> {
  const user = useCookie<string | null>("fixture-user");
  const incoming = {
    status: null as string | null,
    createdSessionId: null as string | null,
    async create({ identifier }: { identifier: string }) {
      incoming.status = "complete";
      incoming.createdSessionId = identifier.startsWith("bob") ? "bob" : "alice";
      return incoming;
    },
  };
  return {
    isLoaded: ref(true),
    signIn: ref(incoming),
    setActive: ref(
      async ({
        session,
        navigate,
      }: {
        session: string;
        navigate?: (value: unknown) => Promise<void>;
      }) => {
        user.value = session;
        await navigate?.({ session: { status: "active", currentTask: null } });
      },
    ),
  } as unknown as ReturnType<typeof Clerk.useSignIn>;
}

export function useSignUp(): ReturnType<typeof Clerk.useSignUp> {
  return { isLoaded: ref(true), signUp: ref({}) } as ReturnType<typeof Clerk.useSignUp>;
}
