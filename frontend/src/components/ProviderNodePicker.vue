<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { api } from '../api.ts';
import type { ProviderNodes } from '../api.ts';
import { isRecord, names } from '../config.ts';
import type { Config } from '../config.ts';
import type { Field, InputValue } from '../fields.ts';
import FieldInput from './FieldInput.vue';

const props = defineProps<{ config: Config; sources: string[]; selected: string; hasGroupFilter: boolean }>();
const emit = defineEmits<{ selectNode: [string] }>();
const requestedSource = ref('');
const providers = computed(() => {
  const entries = props.config['proxy-providers'];
  if (!isRecord(entries)) return new Map<string, Config>();
  const selected = new Map<string, Config>();
  for (const name of props.sources) {
    const provider = entries[name];
    if (Object.hasOwn(entries, name) && isRecord(provider)) selected.set(name, provider);
  }
  return selected;
});
const source = computed(() => providers.value.has(requestedSource.value) ? requestedSource.value : providers.value.keys().next().value ?? '');
const provider = computed(() => providers.value.get(source.value));
const cached = ref<ProviderNodes>();
const loading = ref(false);
const error = ref('');
let requestID = 0;

const loaded = computed(() => provider.value?.type === 'inline' || Array.isArray(cached.value?.nodes));
const nodes = computed(() => {
  const entry = provider.value;
  const candidates = entry?.type === 'inline' ? names({ proxies: entry.payload }, 'proxies') : cached.value?.nodes ?? [];
  const override = isRecord(entry?.override) ? entry.override : {};
  const prefix = typeof override['additional-prefix'] === 'string' ? override['additional-prefix'] : '';
  const suffix = typeof override['additional-suffix'] === 'string' ? override['additional-suffix'] : '';
  return [...new Set(candidates.map(name => prefix + name + suffix))];
});
const fetchedAt = computed(() => cached.value?.fetchedAt ? new Date(cached.value.fetchedAt).toLocaleString('zh-CN') : '');

const sourceField = computed<Field>(() => ({
  key: 'source', label: '节点来源', kind: 'select', required: true,
  options: [...providers.value.keys()].map(name => [name, name]),
}));
const nodeField = computed<Field>(() => ({
  key: 'node', label: '订阅节点', kind: 'select', placeholder: '选择后填入默认节点',
  options: nodes.value.map(name => [name, name]),
}));
const hasAdvancedOptions = computed(() => {
  const override = provider.value?.override;
  return props.hasGroupFilter || Boolean(provider.value?.filter || provider.value?.['exclude-filter'] ||
    provider.value?.['exclude-type'] || (isRecord(override) && (override['proxy-name'] || override['override-expr'])));
});

watch([source, provider], () => {
  requestID++;
  cached.value = undefined;
  loading.value = false;
  error.value = '';
  if (provider.value?.type === 'http') void readNodes(false);
}, { immediate: true, flush: 'sync' });

function selectNode(value: InputValue) {
  if (typeof value === 'string' && nodes.value.includes(value)) emit('selectNode', value);
}

async function readNodes(refresh: boolean) {
  const entry = provider.value;
  if (entry?.type !== 'http' || loading.value) return;
  const currentRequest = ++requestID;
  loading.value = true;
  error.value = '';
  try {
    if (typeof entry.url !== 'string' || !entry.url.trim()) throw new Error('请先填写该订阅源的地址。');
    const header = isRecord(entry.header) ? entry.header : {};
    const agent = Object.entries(header).find(([name]) => name.toLowerCase() === 'user-agent')?.[1];
    const providerAgent = Array.isArray(agent) ? agent[0] : agent;
    const globalAgent = props.config['global-ua'];
    const userAgent = typeof providerAgent === 'string' && providerAgent.trim()
      ? providerAgent : typeof globalAgent === 'string' ? globalAgent : undefined;
    const result = await api.providerNodes({ url: entry.url, userAgent, refresh });
    if (currentRequest === requestID) cached.value = result;
  } catch (cause) {
    if (currentRequest === requestID) error.value = cause instanceof Error ? cause.message : '读取订阅节点失败。';
  } finally {
    if (currentRequest === requestID) loading.value = false;
  }
}
</script>

<template>
  <div v-if="provider" class="provider-probe full">
    <FieldInput :field="sourceField" prefix="default-provider" :model-value="source" @update:model-value="requestedSource = String($event)" />
    <template v-if="provider.type === 'http' || provider.type === 'inline'">
      <button v-if="provider.type === 'http'" class="btn secondary" type="button" :disabled="loading" @click="readNodes(true)">
        {{ loading ? '正在读取…' : loaded ? '刷新节点名单' : '读取订阅节点' }}
      </button>
      <p v-if="fetchedAt" class="muted" role="status">节点名单更新于 {{ fetchedAt }}</p>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <template v-if="loaded">
        <FieldInput v-if="nodes.length" :field="nodeField" prefix="default-provider"
          :model-value="nodes.includes(selected) ? selected : ''" @update:model-value="selectNode" />
        <p v-else class="muted" role="status">该订阅源暂无节点。</p>
        <p v-if="nodes.length && hasAdvancedOptions" class="muted">
          客户端的筛选或高级重命名会影响实际成员。请以客户端里的节点名为准。
        </p>
      </template>
    </template>
    <p v-else class="muted">可在「默认节点」中填写客户端显示的完整节点名。</p>
  </div>
</template>
