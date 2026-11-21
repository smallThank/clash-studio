<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { APIError, api } from '../api.ts';
import { formatRule, isRecord, parseRule } from '../config.ts';
import type { Config, EntityKey } from '../config.ts';
import { applyEntity, displayValue, entityFields, entityValues, formValues, newEntry, readValues } from '../fields.ts';
import type { Field, InputValue } from '../fields.ts';
import FieldInput from './FieldInput.vue';

const props = defineProps<{
  config: Config;
  kind: EntityKey;
  index: number | null;
  existing?: { value: unknown; name: string };
  duplicateName?: string;
}>();

const original = computed<Config>(() => {
  if (!props.existing) return newEntry[props.kind];
  if (props.kind === 'rules') return { ...parseRule(props.existing.value) };
  return { ...(isRecord(props.existing.value) ? props.existing.value : {}), name: props.duplicateName ?? props.existing.name };
});
const type = ref(String(original.value.type ?? ''));
const editingName = computed(() => props.index === null ? undefined : props.existing?.name);
const fields = computed(() => entityFields(props.config, props.kind, type.value, editingName.value));
const originalFields = computed(() => entityFields(props.config, props.kind, String(original.value.type ?? ''), editingName.value));
const fieldSource = computed(() => entityValues(original.value, fields.value, props.kind === 'rules', originalFields.value));
const values = ref(formValues(fields.value, fieldSource.value));
const visibleFields = computed(() => fields.value.map(spec => {
  if (spec.key !== 'default-selected') return spec;
  const members = values.value.proxies;
  const options: Field['options'] = Array.isArray(members) ? [...new Set(members)].map(name => [name, name]) : [];
  return { ...spec, options };
}));
const probing = ref(false);
const probeMessage = ref('');
const probeError = ref('');

function editField(key: string, value: InputValue) {
  values.value[key] = value;
  if (key !== 'type') return;
  const oldExtras = values.value.extras;
  const previous = fieldSource.value.extras;
  type.value = String(value);
  const source = fieldSource.value;
  for (const spec of fields.value) {
    if (!Object.hasOwn(values.value, spec.key)) values.value[spec.key] = displayValue(spec, source[spec.key]);
  }
  const oldSpec = fields.value.find(field => field.key === 'extras');
  if (oldSpec && oldExtras === displayValue(oldSpec, previous)) values.value.extras = displayValue(oldSpec, source.extras);
}

watch(() => values.value.url, () => {
  probeMessage.value = '';
  probeError.value = '';
});

async function probe() {
  const url = String(values.value.url ?? '').trim();
  if (!url) { probeError.value = '请填写原始订阅地址。'; return; }
  probing.value = true;
  probeMessage.value = '';
  probeError.value = '';
  try {
    const result = await api.probe({ mode: 'provider', url, userAgent: String(values.value['probe-user-agent'] ?? '').trim() || undefined });
    if (result.kind !== 'provider') throw new Error('该来源返回完整配置，可在新建配置中导入。');
    probeMessage.value = (result.host ?? '上游地址') + ' · 已识别为节点来源';
  } catch (error) {
    probeError.value = error instanceof APIError && error.status === 401 ? '登录已失效，请重新登录。'
      : error instanceof Error ? error.message : '来源验证失败。';
  } finally {
    probing.value = false;
  }
}

function data() {
  const read = readValues(fields.value, values.value, fieldSource.value);
  if (props.kind === 'rules') {
    if (props.existing?.value && JSON.stringify(read) === JSON.stringify(fieldSource.value)) return String(props.existing.value);
    return formatRule(read);
  }
  if (type.value === 'http' && ['proxy-providers', 'rule-providers'].includes(props.kind)) {
    let valid = false;
    try { valid = ['http:', 'https:'].includes(new URL(String(read.url)).protocol); } catch { /* Provide a form error below. */ }
    if (!valid) throw new Error('请输入 HTTP 或 HTTPS 地址。');
  }
  return applyEntity(read);
}

defineExpose({ data });
</script>

<template>
  <div class="form-grid">
    <FieldInput v-for="field in visibleFields" :key="field.key" prefix="entity" :field="field"
      :model-value="values[field.key]" @update:model-value="editField(field.key, $event)" />
    <div v-if="kind === 'proxy-providers' && type === 'http'" class="provider-probe full">
      <details>
        <summary>验证订阅源</summary>
        <label class="field">请求 User-Agent（可选）
          <input v-model="values['probe-user-agent']" autocomplete="off" placeholder="沿用服务默认值" />
        </label>
        <button type="button" class="btn secondary" :disabled="probing" @click="probe">{{ probing ? '验证中…' : '检查地址' }}</button>
        <p v-if="probeMessage" class="muted" role="status">{{ probeMessage }}</p>
        <p v-if="probeError" class="form-error" role="alert">{{ probeError }}</p>
      </details>
    </div>
  </div>
</template>
