<script setup lang="ts">
import { computed, ref } from 'vue';
import { moveItem } from '../config.ts';
import type { Field, InputValue } from '../fields.ts';
import Icon from './Icon.vue';

const props = defineProps<{ field: Field; prefix: string }>();
const model = defineModel<InputValue>({ default: '' });
const filter = ref('');
const reveal = ref(false);
const inputID = computed(() => props.prefix + '-' + props.field.key);
const selected = computed(() => Array.isArray(model.value) ? model.value : []);
const value = computed({
  get: () => Array.isArray(model.value) ? '' : model.value,
  set: (value: string) => { model.value = value; },
});

const choices = computed(() => {
  const list: [string, string][] = props.field.kind === 'bool' ? [['true', '启用'], ['false', '关闭']] : [...(props.field.options ?? [])];
  if (value.value && !list.some(([key]) => key === value.value)) list.push([value.value, value.value]);
  return list;
});

const referenceOptions = computed(() => {
  const options = [...new Set([...selected.value, ...(props.field.options ?? []).map(([key]) => key)])];
  return options.filter(option => option.toLowerCase().includes(filter.value.toLowerCase()));
});
const lineMatches = computed(() => value.value.split('\n')
  .map((line, index) => ({ line, index }))
  .filter(({ line }) => line.toLowerCase().includes(filter.value.trim().toLowerCase())));

function toggle(option: string) {
  model.value = selected.value.includes(option) ? selected.value.filter(value => value !== option) : [...selected.value, option];
}
</script>

<template>
  <fieldset v-if="field.kind === 'refs'" :id="inputID" class="field reference-field full">
    <legend>{{ field.label }}</legend>
    <small v-if="field.hint">{{ field.hint }}</small>
    <ol v-if="selected.length" class="reference-order">
      <li v-for="(option, index) in selected" :key="index">
        <span>{{ option }}</span>
        <button class="icon-btn" type="button" :aria-label="'上移 ' + option" :disabled="index === 0"
          @click="model = moveItem(selected, index, index - 1)"><Icon name="arrowup" :size="14" /></button>
        <button class="icon-btn" type="button" :aria-label="'下移 ' + option" :disabled="index === selected.length - 1"
          @click="model = moveItem(selected, index, index + 1)"><Icon name="arrowdown" :size="14" /></button>
      </li>
    </ol>
    <label v-if="(field.options?.length ?? 0) > 12" class="search reference-search">
      <Icon name="search" :size="16" />
      <input v-model="filter" type="search" :aria-label="'搜索' + field.label" :placeholder="'搜索' + field.label + '…'" />
    </label>
    <div class="refs">
      <label v-for="option in referenceOptions" :key="option" class="check-option">
        <input type="checkbox" :checked="selected.includes(option)" @change="toggle(option)" />{{ option }}
      </label>
      <span v-if="!referenceOptions.length" class="muted">{{ filter ? '没有匹配的结果' : '暂无可选条目' }}</span>
    </div>
  </fieldset>
  <div v-else class="field" :class="{ full: ['lines', 'yaml', 'url'].includes(field.kind) }">
    <label :for="inputID">{{ field.label }}</label>
    <select v-if="field.kind === 'select' || field.kind === 'bool'" :id="inputID" v-model="value"
      :required="field.required" :aria-describedby="field.hint ? inputID + '-hint' : undefined">
      <option value="">{{ field.placeholder ?? (field.required ? '请选择' : '沿用默认值') }}</option>
      <option v-for="[option, label] in choices" :key="option" :value="option">{{ label }}</option>
    </select>
    <template v-else-if="field.kind === 'lines' || field.kind === 'yaml'">
      <label v-if="field.searchable" class="search">
        <Icon name="search" :size="16" />
        <input v-model="filter" type="search" :aria-label="'搜索' + field.label" :placeholder="'搜索' + field.label + '…'" />
      </label>
      <ul v-if="field.searchable && filter.trim()" class="field-search-results">
        <li v-for="match in lineMatches" :key="match.index">第 {{ match.index + 1 }} 行 · {{ match.line }}</li>
        <li v-if="!lineMatches.length" class="muted">没有匹配的结果</li>
      </ul>
      <textarea :id="inputID" v-model="value" :rows="field.key === 'advanced' ? 11 : 3"
        :class="{ mono: field.kind === 'yaml' }" spellcheck="false" :required="field.required"
        :aria-describedby="field.hint ? inputID + '-hint' : undefined" />
    </template>
    <div v-else :class="{ 'sensitive-field': field.sensitive }">
      <input :id="inputID" :value="value" :type="field.sensitive && !reveal ? 'password' : field.kind"
        :required="field.required" :maxlength="field.kind !== 'number' ? field.max : undefined"
        :min="field.kind === 'number' ? field.min ?? 0 : undefined" :max="field.kind === 'number' ? field.max : undefined"
        :step="field.kind === 'number' ? 1 : undefined" :placeholder="field.placeholder"
        :autocomplete="field.kind === 'password' || field.sensitive ? 'off' : undefined"
        :aria-describedby="field.hint ? inputID + '-hint' : undefined"
        @input="value = ($event.target as HTMLInputElement).value" />
      <button v-if="field.sensitive" class="btn secondary" type="button" :aria-pressed="reveal" @click="reveal = !reveal">
        {{ reveal ? '隐藏' : '显示' }}
      </button>
    </div>
    <small v-if="field.hint" :id="inputID + '-hint'">{{ field.hint }}</small>
  </div>
</template>
