<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue';
import Icon from './Icon.vue';

const props = defineProps<{
  id: string;
  label: string;
  options: [string, string][];
  placeholder?: string;
  required?: boolean;
  maxlength?: number;
  describedby?: string;
}>();
const value = defineModel<string>({ default: '' });
const root = useTemplateRef<HTMLDivElement>('root');
const input = useTemplateRef<HTMLInputElement>('input');
const open = ref(false);
const query = ref('');
const active = ref(-1);
const options = computed(() => props.options.filter(([key, label]) =>
  (key + ' ' + label).toLowerCase().includes(query.value.toLowerCase()),
));
const activeID = computed(() => open.value && active.value >= 0 && active.value < options.value.length
  ? props.id + '-option-' + active.value : undefined);

watch(options, () => { active.value = -1; }, { flush: 'sync' });

function showOptions() {
  query.value = '';
  active.value = -1;
  open.value = true;
}

function edit(event: Event) {
  const nextValue = (event.target as HTMLInputElement).value;
  query.value = nextValue;
  value.value = nextValue;
  active.value = -1;
  open.value = true;
}

function select(option: string) {
  value.value = option;
  open.value = false;
  input.value?.focus();
}

function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && open.value) {
    event.preventDefault();
    event.stopPropagation();
    open.value = false;
    return;
  }
  if (event.key === 'Enter' && open.value) {
    event.preventDefault();
    if (options.value[active.value]) select(options.value[active.value][0]);
    else open.value = false;
    return;
  }
  if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
  event.preventDefault();
  if (!open.value) showOptions();
  const count = options.value.length;
  if (!count) return;
  active.value = event.key === 'ArrowDown'
    ? (active.value + 1) % count : (active.value <= 0 ? count - 1 : active.value - 1);
  void nextTick(() => {
    if (activeID.value) document.getElementById(activeID.value)?.scrollIntoView({ block: 'nearest' });
  });
}

function closeOutside(event: PointerEvent) {
  if (event.target instanceof Node && !root.value?.contains(event.target)) open.value = false;
}

function focusout(event: FocusEvent) {
  if (!(event.relatedTarget instanceof Node) || !root.value?.contains(event.relatedTarget)) open.value = false;
}

onMounted(() => { document.addEventListener('pointerdown', closeOutside); });
onBeforeUnmount(() => { document.removeEventListener('pointerdown', closeOutside); });
</script>

<template>
  <div ref="root" class="editable-select" @focusout="focusout">
    <div class="editable-select-control">
      <input :id="id" ref="input" :value="value" role="combobox" aria-autocomplete="list"
        :aria-expanded="open" :aria-controls="id + '-options'" :aria-activedescendant="activeID"
        :aria-describedby="describedby" :required="required" :maxlength="maxlength" :placeholder="placeholder"
        autocomplete="off" @click="showOptions" @input="edit" @keydown="keydown" />
      <button type="button" class="icon-btn" :aria-label="(open ? '收起' : '展开') + label + '选项'"
        :aria-expanded="open" :aria-controls="id + '-options'" @mousedown.prevent
        @click="open ? open = false : showOptions(); input?.focus()">
        <Icon name="down" :size="16" />
      </button>
    </div>
    <ul v-if="open" :id="id + '-options'" role="listbox" :aria-label="label + '选项'" class="editable-select-options">
      <li v-for="([option, label], index) in options" :id="id + '-option-' + index" :key="option"
        role="option" :aria-selected="option === value" :class="{ active: active === index }"
        @pointermove="active = index" @mousedown.prevent @click="select(option)">
        <span>{{ label }}</span><Icon v-if="option === value" name="check" :size="16" />
      </li>
      <li v-if="!options.length" role="presentation" class="muted">没有匹配的选项，可直接输入节点名。</li>
    </ul>
  </div>
</template>

<style scoped>
.editable-select { min-inline-size: 0; }
.editable-select-control { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: .25rem; }
.editable-select-control > .icon-btn { inline-size: 2.75rem; min-block-size: 2.75rem; border: 1px solid var(--line); }
.editable-select-options {
  list-style: none;
  margin: .25rem 0 0;
  padding: .25rem;
  max-block-size: 12rem;
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: .375rem;
  background: var(--surface);
}
.editable-select-options li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .5rem;
  min-block-size: 2.75rem;
  padding: .5rem .625rem;
  font-weight: 400;
  overflow-wrap: anywhere;
  cursor: pointer;
}
.editable-select-options li[aria-selected="true"], .editable-select-options li.active { background: var(--accent-wash); }
.editable-select-options svg { flex-shrink: 0; }
</style>
