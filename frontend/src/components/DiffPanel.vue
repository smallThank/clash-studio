<script setup lang="ts">
import { computed } from 'vue';
import { configDiff } from '../config.ts';

const props = withDefaults(defineProps<{ before: string; after: string; beforeLabel?: string; afterLabel?: string }>(), {
  beforeLabel: '已发布版本',
  afterLabel: '当前草稿',
});
const changes = computed(() => configDiff(props.before, props.after));
</script>

<template>
  <div v-if="changes.length" class="diff-panel">
    <p>共 {{ changes.length }} 个配置段有差异。</p>
    <details v-for="change in changes" :key="change.key" class="diff-detail">
      <summary><code>{{ change.key }}</code><span>{{ !change.before ? '新增' : !change.after ? '移除' : '修改' }}</span></summary>
      <div class="diff-grid">
        <div><h3>{{ beforeLabel }}</h3><pre tabindex="0" :aria-label="beforeLabel">{{ change.before || '—' }}</pre></div>
        <div><h3>{{ afterLabel }}</h3><pre tabindex="0" :aria-label="afterLabel">{{ change.after || '—' }}</pre></div>
      </div>
    </details>
  </div>
  <p v-else class="muted">配置字段没有差异。</p>
</template>
