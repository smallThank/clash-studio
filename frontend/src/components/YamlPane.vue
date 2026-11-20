<script setup lang="ts">
import { computed } from 'vue';
import type { Issue } from '../api.ts';
import Icon from './Icon.vue';

const props = defineProps<{ yaml: string; published: boolean; canShowPublished: boolean; issues: Issue[]; checking: boolean; checked: boolean }>();
const emit = defineEmits<{ mode: ['draft' | 'published']; copy: []; validate: []; links: []; diff: [] }>();
const lines = computed(() => props.yaml.trimEnd().split('\n').map((raw, index) => {
  const parts = raw.match(/^(\s*(?:-\s+)?)([^:]+)(:\s*)(.*)$/);
  return { raw, number: index + 1, parts: parts && !parts[2].includes(',') ? parts : null };
}));
const errors = computed(() => props.issues.filter(issue => issue.severity === 'error').length);
const label = computed(() => {
  if (props.checking) return '正在检查草稿…';
  if (!props.checked) return '检查草稿';
  if (errors.value) return '发现 ' + errors.value + ' 处错误';
  if (props.issues.length) return '有 ' + props.issues.length + ' 条提示';
  return '草稿结构与引用检查通过';
});
</script>

<template>
  <aside class="preview">
    <div class="preview-header">
      <div class="preview-title"><Icon name="code" :size="20" /><h2>YAML 预览</h2></div>
      <div class="preview-actions">
        <button v-if="canShowPublished" type="button" class="icon-btn" aria-label="比较草稿与已发布版本"
          title="比较草稿与已发布版本" @click="emit('diff')"><Icon name="sliders" :size="16" /></button>
        <button type="button" class="icon-btn" aria-label="复制预览 YAML" title="复制预览 YAML" :disabled="!yaml"
          @click="emit('copy')"><Icon name="copy" :size="16" /></button>
      </div>
    </div>
    <div class="preview-tools">
      <div class="preview-tabs" aria-label="YAML 版本">
        <button type="button" :class="{ active: !published }" :aria-pressed="!published" @click="emit('mode', 'draft')">当前草稿</button>
        <button type="button" :class="{ active: published }" :aria-pressed="published" :disabled="!canShowPublished"
          @click="emit('mode', 'published')">已发布</button>
      </div>
      <span class="line-count">{{ yaml ? lines.length : 0 }} 行</span>
    </div>
    <div class="code-window" tabindex="0" role="region" aria-label="生成的 YAML">
      <div v-for="line in lines" :key="line.number" class="code-line">
        <span class="line-no" aria-hidden="true">{{ line.number }}</span>
        <code v-if="line.parts">{{ line.parts[1] }}<span class="yaml-key">{{ line.parts[2] }}</span>{{ line.parts[3] }}<span class="yaml-val">{{ line.parts[4] }}</span></code>
        <code v-else>{{ line.raw }}</code>
      </div>
    </div>
    <div class="preview-footer">
      <button type="button" class="check-text" :class="{ 'check-error': errors }" @click="emit('validate')">
        <Icon :name="errors ? 'info' : 'shield'" :size="16" />{{ label }}
      </button>
      <button type="button" class="icon-btn" aria-label="管理订阅链接" title="管理订阅链接" @click="emit('links')"><Icon name="link" :size="16" /></button>
    </div>
  </aside>
</template>
