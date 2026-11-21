<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { api } from '../api.ts';
import { isRecord, listFor, parseYAML, policies } from '../config.ts';
import type { Config } from '../config.ts';

const props = defineProps<{ config: Config; busy: boolean }>();
const url = ref('');
const userAgent = ref('');
const yaml = ref('');
const host = ref('');
const error = ref('');
const loading = ref(false);

const nodes = computed(() => {
  if (!yaml.value) return [];
  const used = new Set(policies(props.config));
  return listFor(parseYAML(yaml.value), 'proxies').map(({ index, name, value }) => {
    const skipped = used.has(name);
    used.add(name);
    return { index, name, type: isRecord(value) ? String(value.type ?? '') : '', skipped };
  });
});
const added = computed(() => nodes.value.filter(node => !node.skipped).length);
const canImport = computed(() => !loading.value && added.value > 0);

watch([url, userAgent], () => {
  yaml.value = '';
  error.value = '';
});

async function preview() {
  if (loading.value || props.busy) return;
  const address = url.value.trim();
  if (!address) {
    error.value = '请填写原始订阅地址。';
    return;
  }
  loading.value = true;
  yaml.value = '';
  error.value = '';
  try {
    const result = await api.probe({ mode: 'nodes', url: address, userAgent: userAgent.value.trim() || undefined });
    if (result.kind !== 'nodes' || !result.yaml) throw new Error('订阅未返回可导入的节点。');
    host.value = result.host ?? '';
    yaml.value = result.yaml;
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '读取订阅失败。';
  } finally {
    loading.value = false;
  }
}

defineExpose({ yaml, canImport });
</script>

<template>
  <div class="form-grid">
    <p class="full muted">从订阅下载节点，写入当前草稿。可在代理组的「显式成员」中选择导入的节点。</p>
    <label class="field full">原始订阅地址
      <input v-model="url" required type="url" placeholder="https://" autocomplete="off" :disabled="loading || busy" />
    </label>
    <label class="field full">请求 User-Agent（可选）
      <input v-model="userAgent" autocomplete="off" :disabled="loading || busy" />
    </label>
    <div class="full import-preview">
      <button class="btn secondary" type="button" :disabled="loading || busy" @click="preview">
        {{ loading ? '正在读取…' : '预览节点' }}
      </button>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div v-if="nodes.length" class="import-result">
        <p role="status">
          {{ host ? host + ' · ' : '' }}可导入 {{ added }} 个节点<span v-if="added < nodes.length">，跳过 {{ nodes.length - added }} 个同名节点</span>。
        </p>
        <ul class="node-import-list" aria-label="订阅节点预览" tabindex="0">
          <li v-for="node in nodes" :key="node.index">
            <span>{{ node.name }}</span>
            <span class="muted">{{ node.skipped ? '同名，跳过' : node.type }}</span>
          </li>
        </ul>
      </div>
    </div>
  </div>
</template>

<style scoped>
.node-import-list {
  margin: .5rem 0 0;
  padding: 0;
  max-block-size: 12rem;
  overflow: auto;
  list-style: none;
}
.node-import-list li {
  display: grid;
  grid-template-columns: minmax(0, 1fr) fit-content(40%);
  gap: .625rem;
  padding: .375rem 0;
  border-block-start: 1px solid var(--line);
  font-size: .8125rem;
  overflow-wrap: anywhere;
}
</style>
