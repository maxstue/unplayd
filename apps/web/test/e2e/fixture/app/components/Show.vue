<script setup lang="ts">
import { useAuth } from "../../clerk";
defineOptions({ name: "FixtureShow" });
defineProps<{ when: "signed-in" | "signed-out" }>();
const { isSignedIn } = useAuth();
</script>

<template><slot v-if="isSignedIn === (when === 'signed-in')" /></template>
