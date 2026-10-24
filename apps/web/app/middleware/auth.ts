export default defineNuxtRouteMiddleware(async () => {
  const { isSignedIn } = useAuth();
  if (!isSignedIn.value) return navigateTo("/sign-in");
});
