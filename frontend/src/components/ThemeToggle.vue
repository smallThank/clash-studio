<script setup lang="ts">
import { ref } from 'vue';
import Icon from './Icon.vue';
import { applyTheme, currentTheme } from '../theme.ts';

const dark = ref(currentTheme() === 'dark');

function toggle() {
  dark.value = !dark.value;
  applyTheme(dark.value ? 'dark' : 'light', true);
}
</script>

<template>
  <button class="icon-btn theme-toggle" type="button" aria-label="深色皮肤" :aria-pressed="dark"
    :title="dark ? '切换浅色皮肤' : '切换深色皮肤'" @click="toggle">
    <Icon :name="dark ? 'sun' : 'moon'" />
  </button>
</template>
