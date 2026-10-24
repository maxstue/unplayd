<template>
  <section>
    <h2>Willkommen bei Unplayd</h2>
    <p>Die Grundlage für deine Bibliothek entsteht hier.</p>
    <Show when="signed-out"><NuxtLink to="/sign-up">Konto erstellen</NuxtLink></Show>
    <Show when="signed-in"><NuxtLink to="/dashboard">Zu deinem Konto</NuxtLink></Show>
  </section>
</template>
