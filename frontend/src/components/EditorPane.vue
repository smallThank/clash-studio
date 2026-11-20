<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import { formValues, readValues, settingsFields, settingsValues } from '../fields.ts';
import { isEntityKey, isRecord, listFor, modules, parseRule } from '../config.ts';
import type { Config, EntityKey, ModuleKey, SettingsKey } from '../config.ts';
import type { InputValue } from '../fields.ts';
import FieldInput from './FieldInput.vue';
import Icon from './Icon.vue';

const props = defineProps<{
  config: Config;
  section: ModuleKey;
  busy: boolean;
  saveSettings: (key: SettingsKey, values: Config) => Promise<void>;
}>();
const emit = defineEmits<{
  add: [EntityKey]; edit: [EntityKey, number]; move: [number, number]; bulk: [];
}>();

const current = computed(() => modules.find(module => module.id === props.section)!);
const entity = computed(() => isEntityKey(props.section) ? props.section : null);
const settings = computed(() => isEntityKey(props.section) ? null : props.section);
const fields = computed(() => settings.value ? settingsFields[settings.value] : []);
const initial = computed(() => settings.value ? settingsValues(props.config, settings.value) : {});
const form = ref<Record<string, InputValue>>({});
const feedback = ref('');
const search = ref('');
const page = ref(1);
const pageSize = 24;

watch([initial, fields], () => { form.value = formValues(fields.value, initial.value); feedback.value = ''; }, { immediate: true });
watch(() => props.section, () => { search.value = ''; page.value = 1; });
watch(search, () => { page.value = 1; });

const titles: Record<string, [string, string]> = {
  select: ['手动选择', 'select'], 'url-test': ['自动测速', 'speed'],
  fallback: ['故障切换', 'fallback'], 'load-balance': ['负载均衡', 'fallback'],
};
const rows = computed(() => {
  if (!entity.value) return [];
  return listFor(props.config, entity.value).map(row => {
    const value = isRecord(row.value) ? row.value : {};
    const rule = entity.value === 'rules' ? parseRule(row.value) : undefined;
    const type = rule?.type ?? String(value.type ?? '');
    const [chip, kind] = entity.value === 'proxy-groups' && Object.hasOwn(titles, type) ? titles[type] : [type || '—', 'select'];
    const tags = [
      ...(Array.isArray(value.proxies) ? value.proxies.map(item => ({ item, kind: 'member' })) : []),
      ...(Array.isArray(value.use) ? value.use.map(item => ({ item, kind: 'source' })) : []),
    ];
    let source = value.path || '—';
    if (typeof value.url === 'string') {
      try { source = new URL(value.url).hostname; }
      catch { source = '地址待检查'; }
    }
    return {
      ...row, value, rule, chip, kind, tags, source,
      label: entity.value === 'rules' ? '第 ' + (row.index + 1) + ' 条规则' : row.name || '未命名条目',
      searchText: entity.value === 'rules' ? String(row.value).toLowerCase() : [row.name, type, chip, source].join(' ').toLowerCase(),
    };
  });
});
const filtered = computed(() => rows.value.filter(row => row.searchText.includes(search.value.toLowerCase().trim())));
const pageCount = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)));
const visible = computed(() => filtered.value.slice((page.value - 1) * pageSize, page.value * pageSize));
watch(pageCount, pages => { if (page.value > pages) page.value = pages; });

async function save() {
  if (!settings.value || props.busy) return;
  feedback.value = '';
  try {
    const values = readValues(fields.value, form.value, initial.value);
    await props.saveSettings(settings.value, values);
  } catch (error) {
    feedback.value = error instanceof Error ? error.message : '保存失败。';
  }
}

async function focusRow(index: number) {
  search.value = '';
  await nextTick();
  page.value = Math.floor(index / pageSize) + 1;
  await nextTick();
  document.getElementById('row-' + index)?.querySelector<HTMLButtonElement>('[data-action="edit"]')?.focus();
}

defineExpose({ focusRow });
</script>

<template>
  <section class="editor card">
    <div class="editor-heading"><h2>{{ current.label }}</h2><p>{{ current.detail }}</p></div>
    <form v-if="settings" id="settings-form" class="settings-form" @submit.prevent="save">
      <div class="form-grid">
        <FieldInput v-for="field in fields" :key="settings + '-' + field.key" v-model="form[field.key]" prefix="settings" :field="field" />
      </div>
      <p v-if="settings === 'advanced'" class="extension-note muted">两款客户端的本地覆写设置可能改变最终生效的字段。</p>
      <p v-if="feedback" class="form-error" role="alert">{{ feedback }}</p>
      <button class="btn primary" type="submit" :disabled="busy"><Icon name="check" :size="17" />保存草稿</button>
    </form>
    <template v-else-if="entity">
      <div class="editor-tools">
        <label class="search"><Icon name="search" /><input v-model="search" id="search" type="search"
          :aria-label="'搜索' + current.label" :placeholder="'搜索' + current.label + '…'" /></label>
        <div class="editor-actions">
          <button v-if="entity === 'rules'" type="button" class="btn secondary" :disabled="busy" @click="emit('bulk')">批量添加</button>
          <button type="button" class="btn primary" :disabled="busy" @click="emit('add', entity)"><Icon name="plus" :size="17" />{{ current.add }}</button>
        </div>
      </div>
      <div class="listing">
        <template v-if="visible.length">
          <div class="listing-head" :class="{ 'rules-list': entity === 'rules' }">
            <span>{{ entity === 'rules' ? '顺序' : '名称' }}</span><span>类型</span>
            <span>{{ entity === 'proxy-groups' ? '成员 / 来源' : entity === 'rules' ? '匹配内容与目标' : '来源 / 详情' }}</span>
            <span class="operation-heading">操作</span>
          </div>
          <div v-for="row in visible" :id="'row-' + row.index" :key="row.index" class="listing-row">
            <div class="row-name">
              <Icon v-if="entity === 'rules'" name="route" :size="16" />
              <span v-else class="row-glyph"><Icon :name="current.icon" :size="16" /></span>
              <span>{{ row.label }}</span>
            </div>
            <div><span class="type-chip" :class="row.kind">{{ row.chip }}</span></div>
            <div class="listing-detail">
              <template v-if="entity === 'proxy-groups'">
                <div v-if="row.tags.length" class="members">
                  <span v-for="({ item, kind }, index) in row.tags.slice(0, 3)" :key="index" class="member-chip" :class="kind">
                    <Icon v-if="kind === 'source'" name="rss" :size="12" />{{ item }}
                  </span>
                  <span v-if="row.tags.length > 3" class="extra-count">+{{ row.tags.length - 3 }}</span>
                </div>
                <span v-else class="muted">—</span>
              </template>
              <div v-else-if="entity === 'proxies'" class="row-description">
                {{ row.value.server }}{{ row.value.port ? ':' + row.value.port : '' }}
                <template v-if="row.value['dialer-proxy']"><br />拨号出口 · {{ row.value['dialer-proxy'] }}</template>
              </div>
              <div v-else-if="row.rule" class="row-description">
                <template v-if="row.rule.type === 'RAW'">{{ row.rule.raw }}</template>
                <template v-else>{{ row.rule.content ? row.rule.content + ' → ' : '' }}{{ row.rule.target }}
                  <code v-if="row.rule['no-resolve']">no-resolve</code>
                </template>
              </div>
              <div v-else class="row-description">{{ row.source }}{{ row.value.format || row.value.behavior ? ' · ' + (row.value.format || row.value.behavior) : '' }}</div>
            </div>
            <div class="row-actions">
              <template v-if="entity === 'rules'">
                <button class="icon-btn" type="button" data-action="move-up" :aria-label="'上移' + row.label"
                  :disabled="busy || row.index === 0" @click="emit('move', row.index, row.index - 1)"><Icon name="arrowup" :size="16" /></button>
                <button class="icon-btn" type="button" :aria-label="'下移' + row.label" :disabled="busy || row.index === rows.length - 1"
                  @click="emit('move', row.index, row.index + 1)"><Icon name="arrowdown" :size="16" /></button>
              </template>
              <button class="icon-btn" type="button" data-action="edit" :aria-label="'编辑' + row.label" :title="'编辑' + row.label"
                :disabled="busy" @click="emit('edit', entity, row.index)"><Icon name="edit" :size="16" /></button>
            </div>
          </div>
        </template>
        <div v-else class="empty"><Icon :name="search ? 'search' : 'file'" :size="25" /><p>{{ search ? '没有匹配的结果' : '还没有配置条目' }}</p></div>
      </div>
      <div class="editor-foot">
        <span>{{ search ? '找到 ' + filtered.length + ' / ' : '共 ' }}{{ rows.length }} {{ entity === 'proxy-groups' ? '个代理组' : '条' }}</span>
        <div v-if="pageCount > 1" class="pagination" aria-label="配置条目分页">
          <button class="btn secondary" type="button" :disabled="page <= 1" @click="page--">上一页</button>
          <span>第 {{ page }} / {{ pageCount }} 页</span>
          <button class="btn secondary" type="button" :disabled="page >= pageCount" @click="page++">下一页</button>
        </div>
        <span v-else-if="entity === 'rules'">优先匹配靠上的规则</span>
        <span v-else-if="entity === 'proxy-groups'">代理组可被路由规则引用</span>
      </div>
    </template>
  </section>
</template>
