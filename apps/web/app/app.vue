<template>
  <main>
    <header>
      <NuxtLink to="/" class="brand">Unplayd</NuxtLink>
      <nav aria-label="Hauptnavigation">
        <Show when="signed-in">
          <NuxtLink to="/dashboard">Mein Konto</NuxtLink>
          <UserButton />
        </Show>
        <Show when="signed-out">
          <NuxtLink to="/sign-in">Anmelden</NuxtLink>
        </Show>
      </nav>
    </header>
    <h1>Deine Spiele. Dein Überblick.</h1>
    <p>Dein persönlicher Game Tracker.</p>
    <NuxtPage />
  </main>
</template>

<style>
body {
  margin: 0;
  font-family: system-ui, sans-serif;
  color: #1b2734;
  background: #f8fafb;
}
main {
  max-width: 64rem;
  margin: 4rem auto;
  padding: 0 1.5rem;
}
header,
nav {
  display: flex;
  align-items: center;
  gap: 1.5rem;
}
header {
  justify-content: space-between;
  margin-bottom: 3rem;
}
a {
  color: #164e63;
}
.brand {
  font-size: 1.5rem;
  font-weight: 750;
  text-decoration: none;
}
.auth-page {
  display: flex;
  justify-content: center;
  padding: 1rem 0;
}
.account-id {
  overflow-wrap: anywhere;
  color: #526171;
}
button {
  font: inherit;
  padding: 0.6rem 1rem;
  cursor: pointer;
}
@media (max-width: 480px) {
  main {
    margin-top: 1.5rem;
  }
  nav {
    gap: 0.75rem;
  }
}
</style>
