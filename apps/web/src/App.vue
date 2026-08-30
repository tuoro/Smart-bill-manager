<script setup lang="ts">
import { RouterView, useRoute } from 'vue-router'
import { computed } from 'vue'
import AppShell from './app/AppShell.vue'

const route = useRoute()
const isPublic = computed(() => Boolean(route.meta.public))
</script>

<template>
  <RouterView v-slot="{ Component }">
    <component :is="Component" v-if="isPublic" />
    <AppShell v-else>
      <component :is="Component" />
    </AppShell>
  </RouterView>
</template>
