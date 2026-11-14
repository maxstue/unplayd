<script setup lang="ts">
const user = useCookie<string | null>("fixture-user");
</script>

<template><button type="button" @click="user = null">Abmelden</button></template>
