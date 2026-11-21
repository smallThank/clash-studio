<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { api } from '../api.ts';
import { isRecord, names, parseYAML } from '../config.ts';
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
const nodes = ref<string[]>([]);
const loading = ref(false);
const loaded = ref(false);
const error = ref('');
let requestID = 0;

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
  nodes.value = [];
  loaded.value = false;
  loading.value = false;
  error.value = '';
}, { flush: 'sync' });

function selectNode(value: InputValue) {
  if (typeof value === 'string' && nodes.value.includes(value)) emit('selectNode', value);
}

async function readNodes() {
  const entry = provider.value;
  if (!entry || loading.value) return;
  const currentRequest = ++requestID;
  loading.value = true;
  loaded.value = false;
  nodes.value = [];
  error.value = '';
  try {
    let candidates = names({ proxies: entry.payload }, 'proxies');
    if (entry.type === 'http') {
      if (typeof entry.url !== 'string' || !entry.url.trim()) throw new Error('请先填写该订阅源的地址。');
      const header = isRecord(entry.header) ? entry.header : {};
      const agent = Object.entries(header).find(([name]) => name.toLowerCase() === 'user-agent')?.[1];
      const userAgent = Array.isArray(agent) ? agent[0] : agent;
      const result = await api.probe({ mode: 'nodes', url: entry.url, userAgent: typeof userAgent === 'string' ? userAgent : undefined });
      if (result.kind !== 'nodes' || !result.yaml) throw new Error('订阅未返回节点列表。');
      candidates = names(parseYAML(result.yaml), 'proxies');
    }
    if (currentRequest !== requestID) return;
    const override = isRecord(entry.override) ? entry.override : {};
    const prefix = typeof override['additional-prefix'] === 'string' ? override['additional-prefix'] : '';
    const suffix = typeof override['additional-suffix'] === 'string' ? override['additional-suffix'] : '';
    nodes.value = [...new Set(candidates.map(name => prefix + name + suffix))];
    loaded.value = true;
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
      <button class="btn secondary" type="button" :disabled="loading" @click="readNodes">
        {{ loading ? '正在读取…' : '读取订阅节点' }}
      </button>
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
