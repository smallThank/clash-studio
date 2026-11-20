<script setup lang="ts">
import { computed, nextTick, onMounted, ref, shallowRef, useTemplateRef } from 'vue';
import { APIError, api, clearSession } from './api.ts';
import type { Account, Draft, Issue, Profile, RefreshPreview, Rename, SubscriptionLink, Target, Version } from './api.ts';
import { basicKeys, draftPatch, insertRules, isEntityKey, listFor, modules, moveItem, parseRule, parseYAML, targets, withEntity } from './config.ts';
import type { Config, EntityKey, ModuleKey, SettingsKey } from './config.ts';
import { applySettings } from './fields.ts';
import Brand from './components/Brand.vue';
import DiffPanel from './components/DiffPanel.vue';
import EditorPane from './components/EditorPane.vue';
import EntityForm from './components/EntityForm.vue';
import Icon from './components/Icon.vue';
import ImportForm from './components/ImportForm.vue';
import ModalDialog from './components/ModalDialog.vue';
import YamlPane from './components/YamlPane.vue';

const account = shallowRef<Account | null>(null);
const authMode = ref<'login' | 'register'>('login');
const username = ref('');
const password = ref('');
const authError = ref('');
const booting = ref(true);
const bootError = ref('');
const busy = ref(false);
const profiles = shallowRef<Profile[]>([]);
const selectedID = ref('');
const profile = computed(() => profiles.value.find(item => item.id === selectedID.value));
const draft = shallowRef<Draft | null>(null);
const configuration = shallowRef<Config>({});
const section = ref<ModuleKey>('proxy-groups');
const mobileView = ref<'editor' | 'preview'>('editor');
const previewMode = ref<'draft' | 'published'>('draft');
const publishedYAML = ref('');
const yaml = computed(() => previewMode.value === 'published' ? publishedYAML.value : draft.value?.yaml ?? '');
const issues = shallowRef<Issue[]>([]);
const checking = ref(false);
const checked = ref(false);
const editor = useTemplateRef<InstanceType<typeof EditorPane>>('editor');
let checkSequence = 0;

type Popup = 'new' | 'manage' | 'remove-profile' | 'entity' | 'remove-entity' | 'bulk' | 'check' | 'publish' | 'links' |
  'new-link' | 'revoke' | 'link-content' | 'versions' | 'restore' | 'diff' | 'refresh' | 'account';
const modal = ref<Popup | null>(null);
const modalError = ref('');
const entity = useTemplateRef<InstanceType<typeof EntityForm>>('entity');
const importer = useTemplateRef<InstanceType<typeof ImportForm>>('importer');
const editingKind = ref<EntityKey>('proxy-groups');
const editingIndex = ref<number | null>(null);
const currentEntry = shallowRef<ReturnType<typeof listFor>[number]>();
const duplicateName = ref('');
const profileName = ref('');
const profileTarget = ref<Target>('both');
const text = ref('');
const linkName = ref('');
const links = shallowRef<SubscriptionLink[]>([]);
const activeLink = shallowRef<SubscriptionLink>();
const linkURL = ref('');
const versions = shallowRef<Version[]>([]);
const version = shallowRef<Version>();
const comparison = shallowRef<{ before: string; beforeLabel: string }>();
const refresh = shallowRef<RefreshPreview>();
const resolutions = ref<Record<string, 'draft' | 'upstream'>>({});
const notification = ref('');
let notificationTimer: ReturnType<typeof setTimeout> | undefined;

const modalTitles: Record<Popup, string> = {
  new: '新建配置', manage: '配置管理', 'remove-profile': '删除配置', entity: '编辑条目', 'remove-entity': '删除条目',
  bulk: '批量添加规则', check: '配置校验', publish: '发布订阅', links: '订阅链接', 'new-link': '新建订阅链接', revoke: '吊销订阅链接',
  'link-content': '订阅地址', versions: '发布记录', restore: '恢复历史版本', diff: '比较版本', refresh: '刷新原始订阅', account: '当前账户',
};
const entryLabel = computed(() => editingKind.value === 'rules' ? '第 ' + ((currentEntry.value?.index ?? 0) + 1) + ' 条规则' : currentEntry.value?.name);
const modalTitle = computed(() => {
  if (modal.value === 'entity') {
    const module = modules.find(module => module.id === editingKind.value)!;
    return editingIndex.value !== null ? '编辑' + (editingKind.value === 'rules' ? entryLabel.value : module.label)
      : currentEntry.value ? '复制' + module.label : module.add ?? '新建条目';
  }
  if (modal.value === 'links' && profile.value?.publishedVersion) return '订阅链接 · v' + profile.value.publishedVersion;
  return modal.value ? modalTitles[modal.value] : '';
});
const submitLabels: Partial<Record<Popup, string>> = {
  new: '创建配置', manage: '保存', 'remove-profile': '确认删除', entity: '保存到草稿', 'remove-entity': '确认删除',
  bulk: '添加到草稿', publish: '确认发布', 'new-link': '生成订阅地址', revoke: '确认吊销', restore: '恢复到草稿', refresh: '应用更新',
};

function notify(message: string) {
  notification.value = message;
  clearTimeout(notificationTimer);
  notificationTimer = setTimeout(() => { notification.value = ''; }, 3500);
}
function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '操作失败，请重试。';
}
function resetSession() {
  checkSequence++;
  clearSession();
  account.value = null;
  profiles.value = [];
  selectedID.value = '';
  draft.value = null;
  configuration.value = {};
  issues.value = [];
  publishedYAML.value = '';
  comparison.value = undefined;
  refresh.value = undefined;
  linkURL.value = '';
  links.value = [];
  currentEntry.value = undefined;
  text.value = '';
  password.value = '';
  closeDialog();
}
function handleError(error: unknown, dialog = false) {
  if (error instanceof APIError && error.status === 401 && account.value) {
    resetSession();
    authError.value = '会话已结束，请重新登录。';
  } else if (dialog) {
    modalError.value = errorMessage(error);
  } else {
    notify(errorMessage(error));
  }
}
async function run(operation: () => Promise<void>, inDialog = modal.value !== null) {
  if (busy.value) return;
  busy.value = true;
  modalError.value = '';
  try { await operation(); }
  catch (error) { handleError(error, inDialog); }
  finally { busy.value = false; }
}
function setProfile(value: Profile) {
  const exists = profiles.value.some(item => item.id === value.id);
  profiles.value = exists ? profiles.value.map(item => item.id === value.id ? value : item) : [...profiles.value, value];
}
function setDraft(value: Draft) {
  const editorData = parseYAML(value.yaml);
  draft.value = value;
  configuration.value = editorData;
  issues.value = [];
  checked.value = false;
  previewMode.value = 'draft';
  if (profile.value) setProfile({ ...profile.value, draftRev: value.draftRev });
}
async function checkDraft(id: string, rev: number) {
  const sequence = ++checkSequence;
  checking.value = true;
  try {
    const result = await api.preview(id, { rev });
    if (sequence !== checkSequence || selectedID.value !== id || draft.value?.draftRev !== rev) return;
    issues.value = result.issues ?? [];
    checked.value = true;
  } catch (error) {
    if (sequence === checkSequence) handleError(error);
  } finally {
    if (sequence === checkSequence) checking.value = false;
  }
}
function showSection(key: ModuleKey) {
  section.value = key;
  mobileView.value = 'editor';
}
async function openProfile(id: string) {
  const data = await api.draft(id);
  selectedID.value = id;
  setDraft(data);
  publishedYAML.value = '';
  showSection('proxy-groups');
  await checkDraft(id, data.draftRev);
}
async function loadProfiles() {
  const loaded: Profile[] = [];
  let offset = 0;
  while (true) {
    const result = await api.profiles(offset);
    loaded.push(...(result.profiles ?? []));
    if (result.nextOffset === undefined || result.nextOffset <= offset) break;
    offset = result.nextOffset;
  }
  profiles.value = loaded;
  selectedID.value = '';
  if (profiles.value.length) await openProfile(profiles.value[0].id);
}
async function boot() {
  booting.value = true;
  bootError.value = '';
  try {
    account.value = await api.me();
    await loadProfiles();
  } catch (error) {
    resetSession();
    if (!(error instanceof APIError) || error.status !== 401) bootError.value = errorMessage(error);
  } finally { booting.value = false; }
}
onMounted(boot);

async function authenticate() {
  if (busy.value) return;
  busy.value = true;
  authError.value = '';
  try {
    const credentials = { username: username.value.trim(), password: password.value };
    if (!credentials.username) throw new Error('请填写用户名。');
    account.value = await (authMode.value === 'login' ? api.login(credentials) : api.register(credentials));
    password.value = '';
    await loadProfiles();
  } catch (error) {
    if (account.value) resetSession();
    authError.value = errorMessage(error);
  } finally { busy.value = false; }
}
function toggleAuth() {
  authMode.value = authMode.value === 'login' ? 'register' : 'login';
  authError.value = '';
  password.value = '';
}
async function logout() {
  return run(async () => {
    await api.logout();
    resetSession();
    authMode.value = 'login';
  });
}
function openDialog(value: Popup) {
  modal.value = value;
  modalError.value = '';
}
function closeDialog() {
  modal.value = null;
  modalError.value = '';
  linkURL.value = '';
  text.value = '';
  currentEntry.value = undefined;
  comparison.value = undefined;
  refresh.value = undefined;
}
function openManager() {
  if (!profile.value) return;
  profileName.value = profile.value.name;
  profileTarget.value = profile.value.target;
  openDialog('manage');
}
function openEditor(kind: EntityKey, index: number | null) {
  editingKind.value = kind;
  editingIndex.value = index;
  currentEntry.value = index !== null ? listFor(configuration.value, kind)[index] : undefined;
  duplicateName.value = '';
  openDialog('entity');
}

function setDuplicate() {
  const row = currentEntry.value;
  if (!row) return;
  if (editingKind.value !== 'rules') {
    const existing = new Set(listFor(configuration.value, editingKind.value).map(entry => entry.name));
    let count = 1;
    let suffix = '（副本）';
    while (existing.has(row.name.slice(0, 80 - suffix.length) + suffix)) suffix = '（副本 ' + ++count + '）';
    duplicateName.value = row.name.slice(0, 80 - suffix.length) + suffix;
  }
  editingIndex.value = null;
  modalError.value = '';
}
async function saveDraft(next: Config, rename?: Rename) {
  if (!profile.value || !draft.value) return;
  const id = profile.value.id;
  const patch = draftPatch(configuration.value, next, rename);
  if (!Object.keys(patch.changes).length && !patch.remove.length) return;
  const saved = await api.patchDraft(id, draft.value.draftRev, patch);
  setDraft(saved);
  await checkDraft(id, saved.draftRev);
}
async function saveSettings(key: SettingsKey, values: Config) {
  if (busy.value) return;
  busy.value = true;
  try {
    await saveDraft(applySettings(configuration.value, key, values));
    notify('已保存到草稿。');
  } catch (error) {
    if (error instanceof APIError && error.status === 401) handleError(error);
    throw error;
  } finally { busy.value = false; }
}
async function moveRule(from: number, to: number) {
  return run(async () => {
    const rules = listFor(configuration.value, 'rules').map(row => String(row.value));
    await saveDraft({ ...configuration.value, rules: moveItem(rules, from, to) });
    await editor.value?.focusRow(to);
    notify('规则顺序已保存。');
  });
}
async function moveFallback() {
  const rules = listFor(configuration.value, 'rules').map(row => String(row.value));
  const index = rules.findIndex(value => parseRule(value).type === 'MATCH');
  if (index < 0) return;
  closeDialog();
  await moveRule(index, rules.length - 1);
}
async function showIssues() {
  if (!profile.value || !draft.value) return;
  if (!checked.value) await checkDraft(profile.value.id, draft.value.draftRev);
  if (account.value) openDialog('check');
}
function locate(issue: Issue) {
  const match = issue.path.match(/^([^.[/]+)(?:\[(\d+|".*?"|'.*?')\]|[./]([^.[/]+))?/);
  const root = match?.[1] ?? '';
  const field = match?.[2] ?? match?.[3] ?? '';
  const key = modules.find(module => module.id === root)?.id ?? (basicKeys.includes(root) ? 'basics' : 'advanced');
  closeDialog();
  showSection(key);
  void nextTick(async () => {
    if (isEntityKey(key)) {
      const index = /^\d+$/.test(field) ? Number(field)
        : listFor(configuration.value, key).find(row => row.name === field.replace(/^['"]|['"]$/g, ''))?.index;
      if (index !== undefined) await editor.value?.focusRow(index);
    } else {
      document.getElementById('settings-' + (key === 'dns' ? field || 'enable' : key === 'basics' ? root : 'advanced'))?.focus();
    }
  });
}

async function download() {
  if (!profile.value || !draft.value) return;
  const original = profile.value;
  return run(async () => {
    try {
      const blob = await api.download(original.id, draft.value!.draftRev);
      const anchor = document.createElement('a');
      const url = URL.createObjectURL(blob);
      anchor.href = url;
      anchor.download = original.name.replace(/[\\/:*?"<>|]/g, '-') + '.yaml';
      anchor.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      notify('已导出草稿 YAML。');
    } catch (error) {
      if (error instanceof APIError && error.issues.length) {
        issues.value = error.issues;
        checked.value = true;
        openDialog('check');
        return;
      }
      throw error;
    }
  });
}
async function showVersion(mode: 'draft' | 'published') {
  if (mode === 'draft') { previewMode.value = mode; return; }
  const current = profile.value;
  if (!current?.publishedVersion) return;
  return run(async () => {
    publishedYAML.value = (await api.preview(current.id, { version: current.publishedVersion! })).yaml;
    previewMode.value = 'published';
  });
}
async function copy(content: string, message: string) {
  try {
    await navigator.clipboard.writeText(content);
    notify(message);
  } catch {
    linkURL.value = content;
    openDialog('link-content');
    void nextTick(() => document.querySelector<HTMLTextAreaElement>('#copy-content')?.select());
  }
}
async function showLinks(id = profile.value?.id) {
  if (!id) return;
  links.value = profile.value?.publishedVersion ? (await api.links(id)).links ?? [] : [];
  openDialog('links');
}
async function openPublish() {
  if (!profile.value || !draft.value) return;
  if (!checked.value) await checkDraft(profile.value.id, draft.value.draftRev);
  if (!account.value) return;
  openDialog(issues.value.some(issue => issue.severity === 'error') ? 'check' : 'publish');
}
async function showLink(link: SubscriptionLink) {
  if (!profile.value) return;
  const result = await api.linkURL(profile.value.id, link.id);
  linkURL.value = result.url;
  openDialog('link-content');
}
async function openCompare(value?: Version) {
  if (!profile.value || !draft.value) return;
  const number = value?.version ?? profile.value.publishedVersion;
  if (!number) return;
  comparison.value = { before: (await api.preview(profile.value.id, { version: number })).yaml, beforeLabel: '已发布 v' + number };
  openDialog('diff');
}
async function openVersions() {
  if (!profile.value) return;
  versions.value = (await api.versions(profile.value.id)).versions ?? [];
  openDialog('versions');
}
async function openRefresh() {
  if (!profile.value) return;
  refresh.value = await api.previewRefresh(profile.value.id);
  resolutions.value = {};
  openDialog('refresh');
}

async function submitDialog() {
  const operation = modal.value;
  return run(async () => {
    if (operation === 'new') {
      if (!importer.value) return;
      const created = await api.createProfile(importer.value.data());
      setProfile(created);
      await openProfile(created.id);
      closeDialog();
      notify('已创建配置。');
      return;
    }
    if (!profile.value || !draft.value) return;
    const id = profile.value.id;
    const rev = draft.value.draftRev;
    let feedback = '';
    switch (operation) {
      case 'manage': {
        if (!profileName.value.trim()) throw new Error('请填写配置名称。');
        setProfile(await api.updateProfile(id, { name: profileName.value.trim(), target: profileTarget.value }));
        feedback = '配置已更新。';
        break;
      }
      case 'remove-profile': {
        await api.deleteProfile(id);
        profiles.value = profiles.value.filter(item => item.id !== id);
        selectedID.value = '';
        if (profiles.value.length) await openProfile(profiles.value[0].id);
        else { draft.value = null; configuration.value = {}; }
        feedback = '配置已删除。';
        break;
      }
      case 'entity': {
        if (!entity.value) return;
        const data = entity.value.data();
        const key = editingKind.value;
        const old = currentEntry.value;
        const rename = key !== 'rules' && typeof data !== 'string' && editingIndex.value !== null && old && data.name !== old.name
          ? { kind: key, from: old.name, to: String(data.name) } : undefined;
        await saveDraft(withEntity(configuration.value, key, editingIndex.value, data), rename);
        feedback = '已保存到草稿。';
        break;
      }
      case 'remove-entity':
        if (editingIndex.value === null) return;
        await saveDraft(withEntity(configuration.value, editingKind.value, editingIndex.value, null));
        feedback = '已删除条目。';
        break;
      case 'bulk': {
        const added = text.value.split('\n').map(line => line.trim()).filter(line => line && !line.startsWith('#'));
        if (!added.length) throw new Error('请填写至少一条规则。');
        const rules = listFor(configuration.value, 'rules').map(row => String(row.value));
        await saveDraft({ ...configuration.value, rules: insertRules(rules, added) });
        feedback = '已添加 ' + added.length + ' 条规则。';
        break;
      }
      case 'publish': {
        try {
          const version = await api.publish(id, rev);
          setProfile(await api.profile(id));
          await showLinks(id);
          notify('已发布 v' + version.version + '。');
          return;
        } catch (error) {
          if (error instanceof APIError && error.issues.length) {
            issues.value = error.issues;
            checked.value = true;
            openDialog('check');
            return;
          }
          throw error;
        }
      }
      case 'new-link': {
        if (!linkName.value.trim()) throw new Error('请填写链接名称。');
        await api.addLink(id, linkName.value.trim());
        await showLinks(id);
        return;
      }
      case 'revoke': {
        if (!activeLink.value) return;
        await api.revokeLink(id, activeLink.value.id);
        await showLinks(id);
        notify('已吊销链接。');
        return;
      }
      case 'restore': {
        if (!version.value) return;
        setDraft(await api.restore(id, version.value.version, rev));
        await checkDraft(id, draft.value.draftRev);
        feedback = '历史版本已恢复到草稿。';
        break;
      }
      case 'refresh': {
        if (!refresh.value) return;
        if (refresh.value.conflicts.some(item => !Object.hasOwn(resolutions.value, item.path))) throw new Error('请为每个冲突选择采用的值。');
        setDraft(await api.applyRefresh(id, refresh.value.id, refresh.value.draftRev, resolutions.value));
        await checkDraft(id, draft.value.draftRev);
        feedback = '上游更新已写入草稿。';
        break;
      }
    }
    closeDialog();
    if (feedback) notify(feedback);
  }, true);
}
async function duplicateProfile() {
  if (!profile.value) return;
  const id = profile.value.id;
  return run(async () => {
    const copied = await api.duplicateProfile(id);
    setProfile(copied);
    await openProfile(copied.id);
    closeDialog();
    notify('已复制配置。');
  });
}
function timeLabel(value: string) {
  return value ? new Date(value).toLocaleString('zh-CN') : '';
}
</script>

<template>
  <main v-if="booting || bootError" class="auth-page">
    <Brand />
    <div class="auth-card">
      <p v-if="booting" role="status">正在加载配置台…</p>
      <template v-else><p class="form-error" role="alert">{{ bootError }}</p><button class="btn primary" @click="boot">重新连接</button></template>
    </div>
  </main>
  <main v-else-if="!account" class="auth-page">
    <Brand />
    <div class="auth-card">
      <h1>{{ authMode === 'login' ? '登录配置台' : '注册账号' }}</h1>
      <p>管理代理配置、导出 YAML 和发布订阅。</p>
      <form id="auth-form" class="form-grid" @submit.prevent="authenticate">
        <label class="field full">用户名<input v-model="username" required maxlength="32" autocomplete="username" /></label>
        <label class="field full">密码<input v-model="password" type="password" required
          :autocomplete="authMode === 'login' ? 'current-password' : 'new-password'" /></label>
        <p v-if="authError" class="form-error full" role="alert">{{ authError }}</p>
        <button class="btn primary full" type="submit" :disabled="busy">{{ authMode === 'login' ? '登录' : '注册并进入' }}</button>
      </form>
      <button class="btn bare" type="button" :disabled="busy" @click="toggleAuth">{{ authMode === 'login' ? '创建账号' : '返回登录' }}</button>
    </div>
  </main>
  <div v-else class="app-shell">
    <aside class="sidebar">
      <Brand />
      <div class="profile-nav">
        <div class="nav-heading"><span>我的配置</span>
          <button class="icon-btn" type="button" aria-label="新建配置" title="新建配置" :disabled="busy" @click="openDialog('new')">
            <Icon name="plus" :size="16" />
          </button>
        </div>
        <button v-for="item in profiles" :key="item.id" type="button" class="nav-item profile-item" :class="{ current: item.id === selectedID }"
          :aria-current="item.id === selectedID ? 'true' : undefined" :disabled="busy" @click="run(() => openProfile(item.id), false)">
          <Icon name="file" :size="17" /><span>{{ item.name }}</span>
        </button>
      </div>
      <nav v-if="profile" class="module-nav" aria-label="配置模块">
        <div v-for="group in ['基础', '代理', '分流']" :key="group" class="nav-category">
          <div class="nav-heading">{{ group }}</div>
          <button v-for="item in modules.filter(module => module.group === group)" :key="item.id" type="button" class="nav-item"
            :class="{ selected: section === item.id }" :aria-current="section === item.id ? 'page' : undefined" @click="showSection(item.id)">
            <Icon :name="item.icon" :size="17" /><span>{{ item.label }}</span>
            <span v-if="isEntityKey(item.id)" class="nav-count">{{ listFor(configuration, item.id).length }}</span>
          </button>
        </div>
      </nav>
      <div class="sidebar-foot">
        <button class="btn bare" type="button" :disabled="busy" @click="openDialog('account')"><Icon name="user" /><span>{{ account.username }}</span></button>
      </div>
    </aside>
    <div class="page">
      <header class="topbar">
        <div class="top-context"><Icon name="file" :size="16" /><span class="crumb">配置</span><span class="separator">/</span><strong>{{ profile?.name ?? '我的配置' }}</strong></div>
        <div v-if="profile" class="top-actions">
          <button type="button" class="btn secondary" :disabled="busy || !draft" @click="download"><Icon name="download" :size="17" /><span>下载 YAML</span></button>
          <button type="button" class="btn primary" :disabled="busy || !draft" @click="openPublish"><Icon name="upload" :size="17" /><span>发布订阅</span></button>
        </div>
        <button v-else type="button" class="icon-btn empty-account" aria-label="账户" :disabled="busy" @click="openDialog('account')"><Icon name="user" /></button>
      </header>
      <div v-if="profile" class="mobile-controls">
        <label>配置<select id="mobile-profile" aria-label="选择配置" :value="selectedID" :disabled="busy"
          @change="run(() => openProfile(($event.target as HTMLSelectElement).value), false)">
          <option v-for="item in profiles" :key="item.id" :value="item.id">{{ item.name }}</option>
        </select></label>
        <label>模块<select id="mobile-section" aria-label="选择模块" :value="section"
          @change="showSection(($event.target as HTMLSelectElement).value as ModuleKey)">
          <option v-for="item in modules" :key="item.id" :value="item.id">{{ item.label }}</option>
        </select></label>
        <button class="icon-btn" type="button" aria-label="新建配置" :disabled="busy" @click="openDialog('new')"><Icon name="plus" :size="16" /></button>
        <button class="icon-btn" type="button" aria-label="账户" :disabled="busy" @click="openDialog('account')"><Icon name="user" :size="16" /></button>
      </div>
      <main class="workspace">
        <div v-if="!profile" class="card empty empty-workspace">
          <Icon name="file" :size="28" /><h1>我的配置</h1>
          <p>新建配置，或导入已有的 Clash / Mihomo YAML。</p>
          <button class="btn primary" type="button" :disabled="busy" @click="openDialog('new')"><Icon name="plus" :size="17" />新建配置</button>
        </div>
        <template v-else>
          <div class="profile-banner">
            <div class="profile-title">
              <div class="profile-glyph"><Icon :name="profile.target === 'verge' ? 'server' : 'layers'" :size="25" /></div>
              <div><h1>{{ profile.name }}</h1>
                <div class="profile-meta"><span class="target-label">{{ targets[profile.target] }}</span><span class="meta-dot" />
                  {{ profile.publishedVersion ? '已发布 v' + profile.publishedVersion : '未发布' }}</div>
              </div>
            </div>
            <div class="banner-actions">
              <button type="button" class="btn" :class="issues.length ? 'alert-status' : 'status'" :disabled="busy || !draft" @click="showIssues">
                <Icon :name="issues.length ? 'info' : 'check'" :size="17" />{{ issues.length ? '校验结果 · ' + issues.length : '草稿已保存' }}
              </button>
              <button type="button" class="icon-btn" aria-label="配置管理" title="配置管理" :disabled="busy" @click="openManager">
                <Icon name="more" :size="16" />
              </button>
            </div>
          </div>
          <div class="mobile-tabs">
            <button type="button" class="btn" :class="mobileView === 'editor' ? 'mobile-selected' : 'bare'" :aria-pressed="mobileView === 'editor'"
              @click="mobileView = 'editor'">编辑配置</button>
            <button type="button" class="btn" :class="mobileView === 'preview' ? 'mobile-selected' : 'bare'" :aria-pressed="mobileView === 'preview'"
              @click="mobileView = 'preview'">YAML 预览</button>
          </div>
          <div class="work-grid">
            <EditorPane ref="editor" :key="profile.id" :class="{ 'mobile-active': mobileView === 'editor' }"
              :config="configuration" :section="section" :busy="busy" :save-settings="saveSettings"
              @add="openEditor($event, null)" @edit="openEditor" @move="moveRule" @bulk="openDialog('bulk')" />
            <YamlPane :class="{ 'mobile-active': mobileView === 'preview' }" :yaml="yaml" :published="previewMode === 'published'"
              :can-show-published="Boolean(profile.publishedVersion)" :issues="issues" :checking="checking" :checked="checked"
              @mode="showVersion" @copy="copy(yaml, '已复制 YAML。')" @validate="showIssues" @links="run(() => showLinks(), false)"
              @diff="run(() => openCompare(), false)" />
          </div>
        </template>
      </main>
    </div>
  </div>

  <ModalDialog v-if="modal" :key="modal" :title="modalTitle" :submit="submitLabels[modal]" :busy="busy" :error="modalError"
    @close="closeDialog" @submit="submitDialog">
    <ImportForm v-if="modal === 'new'" ref="importer" />
    <template v-else-if="modal === 'manage'">
      <div class="form-grid">
        <label class="field">配置名称<input v-model="profileName" maxlength="48" required /></label>
        <label class="field">目标客户端
          <select v-model="profileTarget"><option v-for="(label, key) in targets" :key="key" :value="key">{{ label }}</option></select>
        </label>
      </div>
      <p class="muted manager-date">最后保存 · {{ timeLabel(profile?.updatedAt ?? '') }}</p>
      <div class="manager-commands">
        <button type="button" class="btn secondary" :disabled="busy" @click="run(openVersions)">发布记录</button>
        <button type="button" class="btn secondary" :disabled="busy" @click="run(() => showLinks())">订阅链接</button>
        <button v-if="profile?.hasSource" type="button" class="btn secondary" :disabled="busy" @click="run(openRefresh)">刷新原始订阅</button>
      </div>
    </template>
    <p v-else-if="modal === 'remove-profile'">删除「{{ profile?.name }}」及其发布记录、订阅链接？</p>
    <EntityForm v-else-if="modal === 'entity'" ref="entity" :key="String(editingIndex) + duplicateName" :kind="editingKind"
      :index="editingIndex" :config="configuration" :existing="currentEntry" :duplicate-name="duplicateName || undefined" />
    <p v-else-if="modal === 'remove-entity'">删除「{{ entryLabel }}」？引用会在配置校验中显示。</p>
    <label v-else-if="modal === 'bulk'" class="field">
      每行一条路由规则，按粘贴顺序加入。
      <textarea v-model="text" rows="7" required spellcheck="false" class="mono" placeholder="DOMAIN-SUFFIX,example.com,DIRECT" />
    </label>
    <template v-else-if="modal === 'check'">
      <p v-if="checking" class="muted" role="status">正在检查草稿…</p>
      <p v-else-if="!checked" class="muted">草稿校验尚未完成。</p>
      <div v-else-if="!issues.length" class="validation-ok">
        <Icon name="shield" :size="22" />
        <div><strong>草稿结构与引用检查通过</strong><p>已检查主要字段、规则顺序及显式引用。可在目标客户端进一步验证版本兼容性。</p></div>
      </div>
      <template v-for="group in (['error', 'warning'] as const)" v-else :key="group">
        <template v-if="issues.some(issue => issue.severity === group)">
          <h3>{{ group === 'error' ? '需要修复' : '需要检查' }}</h3>
          <ul class="issue-list">
            <li v-for="(issue, index) in issues.filter(issue => issue.severity === group)" :key="index">
              <button type="button" class="issue" @click="locate(issue)"><strong>{{ issue.path || '配置' }}</strong><span>{{ issue.message }}</span></button>
            </li>
          </ul>
        </template>
      </template>
    </template>
    <template v-else-if="modal === 'publish'">
      <p>发布「{{ profile?.name }}」的当前草稿？已有订阅链接会继续使用，并在下次拉取时读取本次发布的配置。</p>
      <p v-if="issues.length" class="muted">当前有 {{ issues.length }} 条校验提示。</p>
    </template>
    <template v-else-if="modal === 'links'">
      <p v-if="!profile?.publishedVersion">发布草稿后，即可查看和管理订阅链接。</p>
      <template v-else>
        <p>将订阅地址粘贴到 OpenClash 或 Clash Verge Rev 的远程订阅。</p>
        <div class="link-list">
          <div v-for="link in links" :key="link.id" class="link-item">
            <strong>{{ link.label }}</strong><span class="muted">{{ link.revokedAt ? '已吊销' : '创建于 ' + timeLabel(link.createdAt) }}</span>
            <template v-if="!link.revokedAt">
              <button class="btn secondary" type="button" :disabled="busy" @click="run(() => showLink(link))">查看链接</button>
              <button class="btn danger" type="button" :disabled="busy" @click="activeLink = link; openDialog('revoke')">吊销</button>
            </template>
          </div>
          <p v-if="!links.length" class="muted">暂无订阅链接。</p>
        </div>
      </template>
    </template>
    <label v-else-if="modal === 'new-link'" class="field">链接名称
      <input v-model="linkName" required maxlength="40" placeholder="例如：客厅路由器" />
    </label>
    <p v-else-if="modal === 'revoke'">吊销「{{ activeLink?.label }}」？对应设备之后无法通过此地址读取配置。</p>
    <label v-else-if="modal === 'link-content'" class="field">复制内容
      <textarea id="copy-content" class="mono" :value="linkURL" rows="4" readonly />
    </label>
    <template v-else-if="modal === 'versions'">
      <div v-for="item in versions" :key="item.version" class="link-item">
        <strong>v{{ item.version }}{{ profile?.publishedVersion === item.version ? ' · 当前发布' : '' }}</strong>
        <span class="muted">{{ timeLabel(item.createdAt) }}</span>
        <button class="btn secondary" type="button" :disabled="busy" @click="run(() => openCompare(item))">比较</button>
        <button class="btn secondary" type="button" :disabled="busy" @click="version = item; openDialog('restore')">恢复</button>
      </div>
      <p v-if="!versions.length" class="muted">暂无发布记录。</p>
    </template>
    <p v-else-if="modal === 'restore'">将 v{{ version?.version }} 的配置复制到当前草稿？已发布版本保留当前内容。</p>
    <DiffPanel v-else-if="modal === 'diff' && comparison && draft" :before="comparison.before" :after="draft.yaml" :before-label="comparison.beforeLabel" />
    <template v-else-if="modal === 'refresh' && refresh && draft">
      <p>更新将写入草稿，发布后才供客户端读取。</p>
      <DiffPanel :before="draft.yaml" :after="refresh.yaml" before-label="当前草稿" after-label="上游更新预览" />
      <label v-for="conflict in refresh.conflicts" :key="conflict.path" class="field refresh-conflict">
        <span><code>{{ conflict.path }}</code> · {{ conflict.message }}</span>
        <select v-model="resolutions[conflict.path]" required>
          <option value="">请选择</option><option value="draft">保留草稿值</option><option value="upstream">采用上游值</option>
        </select>
      </label>
    </template>
    <p v-else-if="modal === 'account'">{{ account?.username }}</p>

    <template #actions>
      <template v-if="modal === 'manage'">
        <button class="btn secondary" type="button" :disabled="busy" @click="duplicateProfile">复制配置</button>
        <button class="btn danger" type="button" :disabled="busy" @click="openDialog('remove-profile')">删除</button>
      </template>
      <template v-if="modal === 'entity' && editingIndex !== null">
        <button class="btn secondary" type="button" :disabled="busy" @click="setDuplicate">复制</button>
        <button class="btn danger" type="button" :disabled="busy" @click="openDialog('remove-entity')">删除</button>
      </template>
      <button v-if="modal === 'check' && issues.some(issue => issue.path.startsWith('rules') && issue.message.includes('MATCH'))"
        class="btn secondary" type="button" :disabled="busy" @click="moveFallback">移至规则末尾</button>
      <template v-if="modal === 'links'">
        <button v-if="profile?.publishedVersion" class="btn primary" type="button" :disabled="busy"
          @click="linkName = ''; openDialog('new-link')"><Icon name="plus" :size="17" />新建链接</button>
        <button v-else class="btn primary" type="button" :disabled="busy" @click="openPublish"><Icon name="upload" :size="17" />发布订阅</button>
      </template>
      <button v-if="modal === 'link-content'" class="btn primary" type="button"
        @click="copy(linkURL, '已复制内容。')"><Icon name="copy" :size="17" />复制</button>
      <button v-if="modal === 'account'" class="btn danger" type="button" :disabled="busy" @click="logout">
        <Icon name="logout" :size="17" />退出登录
      </button>
    </template>
  </ModalDialog>
  <div id="toast" role="status" aria-live="polite" :class="{ visible: notification }">{{ notification }}</div>
</template>
