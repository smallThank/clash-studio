import './style.css';
import {
  modules, initialState, listFor, newProfile, parseRule, parseYAML, toYAML, validate, uuid, isMapping,
} from './model.js';
import {
  settingFields, entityFields, startValues, settingsValues, applySettings, omit, readField, readFields, fieldString,
} from './fields.js';

const app = document.querySelector('#app');
const dialog = document.querySelector('#dialog');
const toast = document.querySelector('#toast');
const storageKey = 'clash-config-studio-demo-v1';
let state;
try {
  const stored = JSON.parse(localStorage.getItem(storageKey));
  state = stored?.profiles?.length ? stored : initialState();
} catch {
  state = initialState();
}
let section = 'proxy-groups';
let mobileView = 'editor';
let previewMode = 'draft';
let query = '';
let authMode = 'login';
let toastTimeout;
let submitDialog;

const active = () => state.profiles.find(profile => profile.id === state.selected) ?? state.profiles[0];
const escape = value => String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]);
const count = (key, config = active().draft) => listFor(config, key).length;
const targets = { openclash: 'OpenClash', verge: 'Clash Verge Rev', both: '两端通用' };
const iconPaths = {
  mark: 'M4 6h9m4 0h3M4 12h3m4 0h9M4 18h11m4 0h1M13 4v4M7 10v4M15 16v4',
  plus: 'M12 5v14M5 12h14',
  down: 'm6 9 6 6 6-6',
  file: 'M6 3h8l4 4v14H6zM14 3v5h4M9 13h6m-6 4h6',
  sliders: 'M4 7h8m4 0h4M4 17h4m4 0h8M12 5v4m-4 6v4',
  globe: 'M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0ZM2 12h20m-10 10c5-5 5-15 0-20-5 5-5 15 0 20Z',
  rss: 'M5 5a14 14 0 0 1 14 14M5 11a8 8 0 0 1 8 8m-8 0h.01',
  server: 'M4 5h16v6H4zm0 8h16v6H4zm3-5h.01M7 16h.01m9-8h1m-1 8h1',
  layers: 'm12 4 9 5-9 5-9-5zm9 9-9 5-9-5m18 4-9 5-9-5',
  box: 'M12 3 3 7.5v9l9 4.5 9-4.5v-9zM3 7.5l9 4.5 9-4.5M12 12v9M7.5 5.3l9 4.5',
  route: 'M5 5h.01m14 14h.01M5 8v6a5 5 0 0 0 5 5h6m3-3v-6a5 5 0 0 0-5-5H8M3 5a2 2 0 1 1 4 0 2 2 0 0 1-4 0Zm14 14a2 2 0 1 1 4 0 2 2 0 0 1-4 0Z',
  code: 'm8 7-5 5 5 5m8-10 5 5-5 5m-3-13-2 16',
  check: 'm5 12 4 4L19 6',
  shield: 'M12 22s7-3 7-10V5l-7-3-7 3v7c0 7 7 10 7 10zm-3-10 2 2 4-4',
  search: 'M16 16l4 4m-2-9a7 7 0 1 1-14 0 7 7 0 0 1 14 0Z',
  edit: 'm15 5 4 4M4 20l5-1L20 8a2.8 2.8 0 0 0-4-4L5 15z',
  copy: 'M9 9h11v11H9zm6-3V4H4v11h2',
  link: 'm10 14 4-4m-5 6-2 2a3.5 3.5 0 1 1-5-5l4-4a3.5 3.5 0 0 1 5 0m4-1 2-2a3.5 3.5 0 1 1 5 5l-4 4a3.5 3.5 0 0 1-5 0',
  upload: 'M12 16V4m-4 4 4-4 4 4M5 14v6h14v-6',
  download: 'M12 4v12m-4-4 4 4 4-4M5 17v3h14v-3',
  more: 'M5 12h.01m7 0h.01m7 0h.01',
  close: 'm6 6 12 12M18 6 6 18',
  arrowup: 'M12 19V5m-5 5 5-5 5 5',
  arrowdown: 'M12 5v14m-5-5 5 5 5-5',
  info: 'M12 8h.01m0 4v4m10-4a10 10 0 1 1-20 0 10 10 0 0 1 20 0Z',
  logout: 'M10 5H4v14h6m3-7h8m-4-4 4 4-4 4',
  user: 'M16 8a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM4 21a8 8 0 0 1 16 0',
};

function icon(name, size = 18) {
  return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="${name === 'more' ? 3.7 : 1.7}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="${iconPaths[name]}"/></svg>`;
}

function button(label, action, { variant = 'secondary', symbol, attrs = '' } = {}) {
  return `<button type="button" class="btn ${variant}" data-action="${action}" ${attrs}>${symbol ? icon(symbol, 17) : ''}<span>${label}</span></button>`;
}
function iconButton(label, action, symbol, attrs = '') {
  return `<button type="button" class="icon-btn" aria-label="${escape(label)}" title="${escape(label)}" data-action="${action}" ${attrs}>${icon(symbol, 16)}</button>`;
}
function brand() {
  return `<div class="brand"><span class="brand-icon">${icon('mark', 23)}</span><span><strong>Clash 配置台</strong><small>原型预览</small></span></div>`;
}
function notify(message) {
  toast.textContent = message;
  toast.classList.add('visible');
  clearTimeout(toastTimeout);
  toastTimeout = setTimeout(() => toast.classList.remove('visible'), 2800);
}
function persist() {
  try {
    localStorage.setItem(storageKey, JSON.stringify(state));
    return true;
  } catch {
    notify('浏览器空间不足，当前修改只保留在此页面。');
    return false;
  }
}
function saveDraft(config) {
  const profile = active();
  profile.draft = config;
  profile.savedAt = Date.now();
  previewMode = 'draft';
  return persist();
}
function showSection(key) {
  section = key;
  query = '';
  mobileView = 'editor';
  render();
}

function sidebar(profile) {
  const folders = state.profiles.map(item => `<button class="nav-item profile-item ${item.id === profile.id ? 'current' : ''}" data-action="switch-profile" data-profile="${escape(item.id)}" ${item.id === profile.id ? 'aria-current="true"' : ''}>${icon('file', 17)}<span>${escape(item.name)}</span></button>`).join('');
  return `<aside class="sidebar">
    ${brand()}
    <div class="profile-nav">
      <div class="nav-heading"><span>我的配置</span>${iconButton('新建配置', 'new-profile', 'plus')}</div>
      ${folders}
    </div>
    <nav aria-label="配置模块" class="module-nav">
      ${['基础', '代理', '分流'].map(group => `<div class="nav-category"><div class="nav-heading">${group}</div>
        ${modules.filter(module => module.group === group).map(module => {
          const selected = module.id === section;
          const amount = module.add ? `<span class="nav-count">${count(module.id)}</span>` : '';
          return `<button class="nav-item ${selected ? 'selected' : ''}" data-action="section" data-section="${module.id}" ${selected ? 'aria-current="page"' : ''}>${icon(module.icon, 17)}<span>${module.label}</span>${amount}</button>`;
        }).join('')}</div>`).join('')}
    </nav>
    <div class="sidebar-foot">${button(escape(state.account), 'account', { variant: 'bare', symbol: 'user' })}</div>
  </aside>`;
}

function header(profile) {
  return `<header class="topbar">
    <div class="top-context">${icon('file', 16)}<span class="crumb">配置</span><span class="separator">/</span><strong>${escape(profile.name)}</strong></div>
    <div class="top-actions">
      ${button('下载 YAML', 'download', { symbol: 'download' })}
      ${button('发布订阅', 'publish', { symbol: 'upload', variant: 'primary' })}
    </div>
  </header>`;
}

function profilePanel(profile, report) {
  const alerts = report.errors.length || report.warnings.length;
  return `<div class="profile-banner">
    <div class="profile-title">
      <div class="profile-glyph">${icon(profile.target === 'verge' ? 'server' : 'layers', 25)}</div>
      <div><h1>${escape(profile.name)}</h1>
      <div class="profile-meta"><span class="target-label">${targets[profile.target] ?? targets.both}</span><span class="meta-dot"></span>${profile.published ? '已发布 v' + profile.published.revision : '未发布'}</div></div>
    </div>
    <div class="banner-actions">
      ${button(alerts ? '校验结果 · ' + (report.errors.length + report.warnings.length) : '草稿已保存', 'validate', { variant: alerts ? 'alert-status' : 'status', symbol: alerts ? 'info' : 'check' })}
      ${iconButton('配置管理', 'manage-profile', 'more')}
    </div>
  </div>`;
}

const groupTypes = {
  select: ['手动选择', 'select'],
  'url-test': ['自动测速', 'speed'],
  fallback: ['故障切换', 'fallback'],
  'load-balance': ['负载均衡', 'fallback'],
};
function tag(label, kind = 'select') {
  return `<span class="type-chip ${kind}">${escape(label)}</span>`;
}
function summary(row, key) {
  const { value, name } = row;
  if (key === 'proxy-groups') {
    const members = Array.isArray(value?.proxies) ? value.proxies : [];
    const sources = Array.isArray(value?.use) ? value.use : [];
    const tags = [...members.map(item => ({ item, kind: 'member' })), ...sources.map(item => ({ item, kind: 'source' }))];
    return tags.length ? `<div class="members">${tags.slice(0, 3).map(({ item, kind }) => `<span class="member-chip ${kind}">${kind === 'source' ? icon('rss', 12) : ''}${escape(item)}</span>`).join('')}${tags.length > 3 ? `<span class="extra-count">+${tags.length - 3}</span>` : ''}</div>` : '<span class="muted">—</span>';
  }
  if (key === 'proxies') {
    return `<div class="row-description">${escape(value?.server)}${value?.port ? ':' + escape(value.port) : ''}${value?.['dialer-proxy'] ? '<br>拨号出口 · ' + escape(value['dialer-proxy']) : ''}</div>`;
  }
  if (key === 'rules') {
    const rule = parseRule(value);
    return `<div class="row-description">${rule.type === 'RAW' ? escape(value) : `${rule.content ? escape(rule.content) + ' → ' : ''}${escape(rule.target)}${rule['no-resolve'] ? ' <code>no-resolve</code>' : ''}`}</div>`;
  }
  if (key === 'proxy-providers' || key === 'rule-providers') {
    let destination = value?.path || '—';
    if (value?.url) {
      try { destination = new URL(value.url).hostname; } catch { destination = '地址待检查'; }
    }
    return `<div class="row-description">${escape(destination)}${value?.format || value?.behavior ? ' · ' + escape(value.format || value.behavior) : ''}</div>`;
  }
  return escape(name);
}

function ruleLabel(row) {
  return '第 ' + (row.index + 1) + ' 条规则';
}
function rowsHTML(key, profile) {
  const rows = listFor(profile.draft, key);
  const filtered = rows.filter(row => {
    const search = key === 'rules' ? String(row.value) : [row.name, row.value?.type].join(' ');
    return search.toLowerCase().includes(query.toLowerCase());
  });
  if (!filtered.length) return `<div class="empty">${icon(query ? 'search' : 'file', 25)}<p>${query ? '没有匹配的结果' : '还没有配置条目'}</p></div>`;
  const heading = key === 'proxy-groups' ? '成员 / 来源' : key === 'rules' ? '匹配内容与目标' : '来源 / 详情';
  return `<div class="listing-head"><span>${key === 'rules' ? '顺序' : '名称'}</span><span>类型</span><span>${heading}</span><span class="operation-heading">操作</span></div>
    ${filtered.map(row => {
      const currentType = key === 'rules' ? parseRule(row.value).type : row.value?.type;
      const [type, kind] = key === 'proxy-groups' ? groupTypes[currentType] ?? [currentType, 'select'] : [currentType, 'select'];
      const attrs = `data-index="${row.index}"`;
      const actions = key === 'rules' ? `${iconButton('上移' + ruleLabel(row), 'move-up', 'arrowup', attrs + (row.index === 0 ? ' disabled' : ''))}${iconButton('下移' + ruleLabel(row), 'move-down', 'arrowdown', attrs + (row.index === rows.length - 1 ? ' disabled' : ''))}` : '';
      const name = key === 'rules' ? ruleLabel(row) : row.name;
      return `<div class="listing-row" id="row-${row.index}"><div class="row-name">${key === 'rules' ? icon('route', 16) : `<span class="row-glyph">${icon(modules.find(module => module.id === key).icon, 16)}</span>`}<span>${escape(name)}</span></div><div>${tag(type || '—', kind)}</div><div class="listing-detail">${summary(row, key)}</div><div class="row-actions">${actions}${iconButton('编辑' + name, 'edit', 'edit', attrs)}</div></div>`;
    }).join('')}`;
}

function fieldHTML(spec, value) {
  const { key, label, kind } = spec;
  const fieldName = key === 'name' ? 'entry-name' : key;
  const display = fieldString(spec, value);
  const hint = spec.hint ? `<small id="hint-${key}">${escape(spec.hint)}</small>` : '';
  const attributes = `id="field-${key}" name="${fieldName}" aria-labelledby="label-${key}" ${spec.required ? 'required' : ''} ${hint ? `aria-describedby="hint-${key}"` : ''}`;
  if (kind === 'refs') {
    const selected = Array.isArray(value) ? value : [];
    const options = [...new Set([...selected, ...(spec.options ?? []).map(([option]) => option)])];
    return `<fieldset class="field refs full"><legend>${escape(label)}</legend>${options.length ? options.map(option => `<label class="check-option"><input type="checkbox" data-ref="${key}" value="${escape(option)}" ${selected.includes(option) ? 'checked' : ''}>${escape(option)}</label>`).join('') : '<span class="muted">暂无可选条目</span>'}</fieldset>`;
  }
  let input;
  if (kind === 'select' || kind === 'bool') {
    const choices = kind === 'bool' ? [['true', '启用'], ['false', '关闭']] : [...spec.options];
    if (display && !choices.some(([value]) => value === display)) choices.push([display, display]);
    input = `<select ${attributes}><option value="">${escape(spec.placeholder ?? (spec.required ? '请选择' : '沿用默认值'))}</option>${choices.map(([option, text]) => `<option value="${escape(option)}" ${option === display ? 'selected' : ''}>${escape(text)}</option>`).join('')}</select>`;
  } else if (kind === 'lines' || kind === 'yaml') {
    input = `<textarea ${attributes} rows="${key === 'advanced' ? 11 : 3}" spellcheck="false" class="${kind === 'yaml' ? 'mono' : ''}">${escape(display)}</textarea>`;
  } else {
    input = `<input type="${kind}" ${attributes} value="${escape(display)}" placeholder="${escape(spec.placeholder ?? '')}" ${kind === 'number' ? 'min="' + (spec.min ?? 0) + '" step="1"' : ''} ${spec.max ? (kind === 'number' ? 'max=' : 'maxlength=') + spec.max : ''} ${kind === 'password' ? 'autocomplete="off"' : ''}>`;
  }
  return `<label class="field ${['lines', 'yaml', 'url'].includes(kind) ? 'full' : ''}" for="field-${key}"><span id="label-${key}">${escape(label)}</span>${input}${hint}</label>`;
}

function editor(profile) {
  const module = modules.find(item => item.id === section);
  const heading = `<div class="editor-heading"><div><h2>${module.title}</h2><p>${module.detail}</p></div></div>`;
  if (!module.add) {
    const values = settingsValues(profile.draft, section);
    return `${heading}<form id="settings-form" class="settings-form"><div class="form-grid">${settingFields[section].map(spec => fieldHTML(spec, values[spec.key])).join('')}</div>
      <p class="form-error" id="settings-error" role="alert" hidden></p><button class="btn primary" type="submit">${icon('check', 17)}保存草稿</button></form>`;
  }
  return `${heading}<div class="editor-tools">
    <label class="search">${icon('search', 18)}<input id="search" type="search" aria-label="搜索${module.label}" placeholder="搜索${module.label}…" value="${escape(query)}"></label>
    <div class="editor-actions">${section === 'rules' ? button('批量添加', 'bulk') : ''}${button(module.add, 'add', { symbol: 'plus', variant: 'primary' })}</div>
  </div>
  <div class="listing" id="listing">${rowsHTML(section, profile)}</div>
  <div class="editor-foot"><span>共 ${count(section, profile.draft)} ${section === 'proxy-groups' ? '个代理组' : '条'}</span>
  ${section === 'rules' ? '<span>优先匹配靠上的规则</span>' : section === 'proxy-groups' ? '<span>代理组可被路由规则引用</span>' : ''}</div>`;
}

function highlight(line) {
  const parts = line.match(/^(\s*(?:-\s+)?)([^:]+)(:\s*)(.*)$/);
  if (!parts || parts[2].includes(',')) return escape(line);
  return `${escape(parts[1])}<span class="yaml-key">${escape(parts[2])}</span>${escape(parts[3])}<span class="yaml-val">${escape(parts[4])}</span>`;
}
function preview(profile, report) {
  const published = previewMode === 'published' && profile.published;
  const yaml = toYAML(published ? profile.published.config : profile.draft);
  const lines = yaml.trimEnd().split('\n');
  const checks = report.errors.length ? '发现 ' + report.errors.length + ' 处错误' : report.warnings.length ? '有 ' + report.warnings.length + ' 条提示' : '草稿结构与引用检查通过';
  return `<aside class="preview ${mobileView === 'preview' ? 'mobile-active' : ''}">
    <div class="preview-header"><div class="preview-title">${icon('code', 20)}<h2>YAML 预览</h2></div>${iconButton('复制预览 YAML', 'copy-yaml', 'copy')}</div>
    <div class="preview-tools">
      <div class="preview-tabs" aria-label="YAML 版本">
        <button type="button" class="${!published ? 'active' : ''}" data-action="preview-draft" aria-pressed="${!published}">当前草稿</button>
        <button type="button" class="${published ? 'active' : ''}" data-action="preview-published" aria-pressed="${Boolean(published)}" ${profile.published ? '' : 'disabled'}>已发布</button>
      </div>
      <span class="line-count">${lines.length} 行</span>
    </div>
    <div class="code-window" tabindex="0" role="region" aria-label="生成的 YAML">${lines.map((line, i) => `<div class="code-line"><span aria-hidden="true" class="line-no">${i + 1}</span><code>${highlight(line)}</code></div>`).join('')}</div>
    <div class="preview-footer"><button type="button" data-action="validate" class="check-text ${report.errors.length ? 'check-error' : ''}">${icon(report.errors.length ? 'info' : 'shield', 16)}${checks}</button>
    ${iconButton('管理订阅链接', 'links', 'link')}</div>
  </aside>`;
}

function authPage() {
  return `<main class="auth-page">${brand()}<div class="auth-card"><h1>${authMode === 'login' ? '登录配置台' : '注册账号'}</h1><p>原型体验。请填写虚构的账号信息。</p>
    <form id="auth-form" class="form-grid">
      <label class="field full">用户名<input name="username" required maxlength="32" autocomplete="username"></label>
      <label class="field full">密码<input name="password" type="password" required autocomplete="${authMode === 'login' ? 'current-password' : 'new-password'}"></label>
      <button class="btn primary full" type="submit">${authMode === 'login' ? '登录' : '注册并进入'}</button>
    </form>
    ${button(authMode === 'login' ? '创建账号' : '返回登录', 'toggle-auth', { variant: 'bare' })}
    </div></main>`;
}
function render() {
  if (!state.account) { app.innerHTML = authPage(); return; }
  const profile = active();
  const report = validate(profile.draft);
  app.innerHTML = `<div class="app-shell">
    ${sidebar(profile)}
    <div class="page">
      ${header(profile)}
      <div class="mobile-controls">
        <label>配置<select id="mobile-profile" aria-label="选择配置">${state.profiles.map(item => `<option value="${escape(item.id)}" ${item.id === profile.id ? 'selected' : ''}>${escape(item.name)}</option>`).join('')}</select></label>
        <label>模块<select id="mobile-section" aria-label="选择模块">${modules.map(module => `<option value="${module.id}" ${module.id === section ? 'selected' : ''}>${module.label}</option>`).join('')}</select></label>
        ${iconButton('新建配置', 'new-profile', 'plus')}${iconButton('账户', 'account', 'user')}
      </div>
      <main class="workspace">
        ${profilePanel(profile, report)}
        <div class="mobile-tabs">${button('编辑配置', 'show-editor', { variant: mobileView === 'editor' ? 'mobile-selected' : 'bare', attrs: 'aria-pressed="' + (mobileView === 'editor') + '"' })}${button('YAML 预览', 'show-preview', { variant: mobileView === 'preview' ? 'mobile-selected' : 'bare', attrs: 'aria-pressed="' + (mobileView === 'preview') + '"' })}</div>
        <div class="work-grid">
          <section class="editor card ${mobileView === 'editor' ? 'mobile-active' : ''}">${editor(profile)}</section>
          ${preview(profile, report)}
        </div>
      </main>
    </div>
  </div>`;
}

function modal(title, body, handleSubmit, { submitLabel = '保存', footer = '' } = {}) {
  submitDialog = handleSubmit;
  dialog.innerHTML = `<div class="dialog-head"><h2 id="dialog-title">${escape(title)}</h2>${iconButton('关闭', 'close-dialog', 'close')}</div>
    <form id="dialog-form"><div class="dialog-body">${body}<p class="form-error" id="dialog-error" role="alert" tabindex="-1" hidden></p></div>
    <div class="dialog-actions">${footer}<div class="dialog-spacer"></div>${button(handleSubmit ? '取消' : '关闭', 'close-dialog')}${handleSubmit ? `<button class="btn primary" type="submit">${escape(submitLabel)}</button>` : ''}</div></form>`;
  if (!dialog.open) dialog.showModal();
  dialog.querySelector('.dialog-body input, .dialog-body select, .dialog-body textarea, .dialog-head button')?.focus();
}

function optionalFields(value, specs) {
  return omit(value, [...specs.map(spec => spec.key), 'name', 'extras']);
}
function openEntity(key, index = null) {
  const draft = active().draft;
  const row = listFor(draft, key)[index];
  const original = row ? (key === 'rules' ? parseRule(row.value) : { ...(isMapping(row.value) ? row.value : {}), name: row.name }) : startValues[key];
  let values = { ...original };
  let specs = entityFields(draft, key, values.type, row?.name);
  if (key !== 'rules') values.extras = optionalFields(original, specs);
  const fields = () => specs.map(spec => fieldHTML(spec, values[spec.key])).join('');
  const isProvider = key.endsWith('-providers');
  const module = modules.find(item => item.id === key);
  const title = row ? '编辑' + (key === 'rules' ? ruleLabel(row) : module.label) : module.add;
  const remove = row ? button('删除', 'remove-entity', { variant: 'danger', attrs: `data-index="${row.index}"` }) : '';
  modal(title, `<div class="form-grid" id="entity-fields">${fields()}</div>`, form => {
    const edited = readFields(form, specs);
    const config = { ...active().draft };
    if (key === 'rules') {
      let text;
      if (edited.type === 'RAW') text = edited.raw;
      else if (edited.type === 'MATCH') text = ['MATCH', edited.target].join(',');
      else text = [edited.type, edited.content, edited.target, ...(edited['no-resolve'] ? ['no-resolve'] : [])].join(',');
      const rules = Array.isArray(config.rules) ? [...config.rules] : [];
      if (row) rules[row.index] = text;
      else insertRules(rules, [text]);
      config.rules = rules;
    } else {
      const data = { ...edited.extras, ...omit(edited, ['extras', ...(isProvider ? ['name'] : [])]) };
      if (edited.name.includes(',')) throw new Error('名称中不能包含逗号。');
      const rows = listFor(config, key);
      if (rows.some(item => item.name === edited.name && item.index !== row?.index)) throw new Error('此名称已存在。');
      if (isProvider) {
        const entries = rows.map(item => [item.name, item.value]);
        if (row) entries[row.index] = [edited.name, data];
        else entries.push([edited.name, data]);
        config[key] = Object.fromEntries(entries);
      } else {
        const entries = rows.map(item => item.value);
        if (row) entries[row.index] = data;
        else entries.push(data);
        config[key] = entries;
      }
    }
    const saved = saveDraft(config);
    dialog.close();
    render();
    if (saved) notify('已保存到草稿');
  }, { footer: remove, submitLabel: '保存到草稿' });

  const form = dialog.querySelector('form');
  form.addEventListener('change', event => {
    if (event.target.name !== 'type') return;
    const error = form.querySelector('#dialog-error');
    try {
      const nextType = event.target.value;
      const previousSpecs = specs;
      const previousValues = readFields(form, previousSpecs);
      const nextSpecs = entityFields(draft, key, nextType, row?.name);
      values = { ...values, ...previousValues, type: nextType };
      if (key !== 'rules') {
        const extras = previousValues.extras ?? {};
        for (const spec of nextSpecs) {
          if (Object.hasOwn(extras, spec.key)) values[spec.key] = extras[spec.key];
        }
        values.extras = optionalFields(extras, nextSpecs);
      }
      specs = nextSpecs;
      form.querySelector('#entity-fields').innerHTML = fields();
      form.elements.type.focus();
      error.hidden = true;
    } catch (exception) {
      event.target.value = values.type;
      error.textContent = exception.message;
      error.hidden = false;
      error.focus();
    }
  });
}
function insertRules(rules, items) {
  const fallback = rules.findIndex(rule => parseRule(rule).type === 'MATCH');
  const regular = items.filter(rule => parseRule(rule).type !== 'MATCH');
  rules.splice(fallback < 0 ? rules.length : fallback, 0, ...regular);
  rules.push(...items.filter(rule => parseRule(rule).type === 'MATCH'));
}

function removeEntity(index) {
  const key = section;
  const rows = listFor(active().draft, key);
  const row = rows[index];
  if (!row) return;
  modal('删除条目', `<p>删除「${escape(key === 'rules' ? ruleLabel(row) : row.name)}」？引用此条目的位置会在校验结果中显示。</p>`, () => {
    const kept = rows.filter(item => item.index !== index);
    const value = key.endsWith('-providers') ? Object.fromEntries(kept.map(item => [item.name, item.value])) : kept.map(item => item.value);
    const saved = saveDraft({ ...active().draft, [key]: value });
    dialog.close();
    render();
    if (saved) notify('已删除条目');
  }, { submitLabel: '确认删除' });
}
function moveRule(index, direction) {
  const rules = [...active().draft.rules];
  const other = index + direction;
  if (other < 0 || other >= rules.length) return;
  [rules[index], rules[other]] = [rules[other], rules[index]];
  const saved = saveDraft({ ...active().draft, rules });
  render();
  document.querySelector(`#row-${other} button[data-action="edit"]`)?.focus();
  if (saved) notify('规则顺序已保存');
}

function openNewProfile() {
  modal('新建配置', `<div class="form-grid">
    ${fieldHTML({ key: 'name', label: '配置名称', kind: 'text', required: true, max: 48 }, '')}
    ${fieldHTML({ key: 'target', label: '目标客户端', kind: 'select', options: Object.entries(targets), required: true }, 'both')}
    <label class="field full"><span id="import-label">导入现有 YAML（可选）</span><input name="file" type="file" accept=".yaml,.yml,application/yaml,text/yaml" aria-labelledby="import-label" aria-describedby="import-hint"><small id="import-hint">导入内容存储在当前浏览器，请使用脱敏文件。</small></label></div>`, async form => {
    const file = form.elements.file.files[0];
    if (file?.size > 1024 * 1024) throw new Error('原型支持导入 1 MB 以内的 YAML。');
    const name = readField(form, { key: 'name' });
    if (!name) throw new Error('请填写配置名称。');
    const profile = newProfile(name, form.elements.target.value, file ? parseYAML(await file.text()) : undefined);
    state.profiles.push(profile);
    state.selected = profile.id;
    const saved = persist();
    dialog.close();
    previewMode = 'draft';
    showSection('proxy-groups');
    if (saved) notify(file ? '已导入 YAML' : '已创建配置');
  }, { submitLabel: '创建配置' });
}
function manageProfile() {
  const profile = active();
  modal('配置管理', `<div class="form-grid">
    ${fieldHTML({ key: 'name', label: '配置名称', kind: 'text', required: true, max: 48 }, profile.name)}
    ${fieldHTML({ key: 'target', label: '目标客户端', kind: 'select', options: Object.entries(targets), required: true }, profile.target)}</div>`, form => {
    const name = readField(form, { key: 'name' });
    if (!name) throw new Error('请填写配置名称。');
    profile.name = name;
    profile.target = form.elements.target.value;
    const saved = persist();
    dialog.close();
    render();
    if (saved) notify('配置已更新');
  }, { footer: button('复制配置', 'duplicate-profile') + button('删除', 'remove-profile', { variant: 'danger', attrs: state.profiles.length < 2 ? 'disabled' : '' }) });
}
function confirmRemoveProfile() {
  modal('删除配置', `<p>删除「${escape(active().name)}」及其模拟发布记录？</p>`, () => {
    state.profiles = state.profiles.filter(profile => profile.id !== state.selected);
    state.selected = state.profiles[0].id;
    const saved = persist();
    dialog.close();
    previewMode = 'draft';
    showSection('proxy-groups');
    if (saved) notify('配置已删除');
  }, { submitLabel: '确认删除' });
}
function validationDialog() {
  const { errors, warnings } = validate(active().draft);
  const issueList = (items, title) => items.length ? `<h3>${title}</h3><ul class="issue-list">${items.map(issue => `<li><button type="button" class="issue" data-action="locate-issue" data-section="${issue.key}" data-name="${escape(issue.name)}"><strong>${escape(modules.find(module => module.id === issue.key)?.label)} · ${escape(issue.name)}</strong><span>${escape(issue.message)}</span></button></li>`).join('')}</ul>` : '';
  modal('配置校验', errors.length || warnings.length ? issueList(errors, '需要修复') + issueList(warnings, '需要检查') : `<div class="validation-ok">${icon('shield', 22)}<div><strong>草稿结构与引用检查通过</strong><p>已检查主要字段、规则顺序及显式引用。目标客户端可进一步检查版本兼容性。</p></div></div>`);
}
function checkDraft() {
  const report = validate(active().draft);
  if (!report.errors.length) return true;
  validationDialog();
  return false;
}
function download() {
  if (!checkDraft()) return;
  const yaml = new Blob([toYAML(active().draft)], { type: 'application/yaml;charset=utf-8' });
  const url = URL.createObjectURL(yaml);
  const link = document.createElement('a');
  link.href = url;
  link.download = active().name.replace(/[\\/:*?"<>|]/g, '-') + '.yaml';
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1500);
  notify('已导出草稿 YAML');
}
async function copy(value, message) {
  try {
    await navigator.clipboard.writeText(value);
    notify(message);
  } catch {
    modal('复制内容', `<label class="field full"><span>请手动复制以下内容</span><textarea readonly rows="6" class="mono">${escape(value)}</textarea></label>`);
    dialog.querySelector('textarea').select();
  }
}
function publish() {
  if (!checkDraft()) return;
  const profile = active();
  const revision = (profile.published?.revision ?? 0) + 1;
  modal('发布订阅', `<p>将「${escape(profile.name)}」的草稿发布为 v${revision}。后续修改可先保存为草稿，已有订阅链接会在再次发布后获得新版本。</p><p class="muted">原型会在当前浏览器记录发布状态并生成示例地址。</p>`, () => {
    profile.published = { revision, config: structuredClone(profile.draft), date: Date.now() };
    if (!profile.links.length) profile.links.push({ token: uuid(), label: '默认订阅' });
    const saved = persist();
    render();
    linksDialog();
    if (saved) notify('已模拟发布 v' + revision);
  }, { submitLabel: '确认发布' });
}
const linkURL = token => 'https://config.example.invalid/s/' + token + '.yaml';
function linksDialog() {
  const profile = active();
  if (!profile.published) {
    modal('订阅链接', '<p>发布草稿后，即可查看和管理订阅链接。</p>', null, { footer: button('发布订阅', 'publish', { variant: 'primary', symbol: 'upload' }) });
    return;
  }
  modal('订阅链接 · v' + profile.published.revision, `<p>以下是原型示例地址。正式版本可粘贴到 OpenClash 或 Clash Verge Rev 的远程订阅。</p>
    <div class="link-list">${profile.links.map(({ token, label }) => `<div class="link-item"><strong>${escape(label)}</strong><code>${escape(linkURL(token))}</code>
    ${iconButton('复制' + label, 'copy-link', 'copy', `data-token="${escape(token)}"`)}${button('吊销', 'revoke', { variant: 'danger', attrs: `data-token="${escape(token)}"` })}</div>`).join('') || '<p class="muted">暂无订阅链接。</p>'}</div>`, null, { footer: button('新建链接', 'new-link', { variant: 'primary', symbol: 'plus' }) });
}

app.addEventListener('input', event => {
  if (event.target.id !== 'search') return;
  query = event.target.value;
  document.querySelector('#listing').innerHTML = rowsHTML(section, active());
});
app.addEventListener('change', event => {
  if (event.target.id === 'mobile-section') showSection(event.target.value);
  if (event.target.id === 'mobile-profile') selectProfile(event.target.value);
});
function selectProfile(id) {
  state.selected = id;
  previewMode = 'draft';
  persist();
  showSection('proxy-groups');
}

document.addEventListener('click', event => {
  const target = event.target.closest('[data-action]');
  if (!target || target.disabled) return;
  const { action, index, profile, token } = target.dataset;
  const number = Number(index);
  switch (action) {
    case 'section': showSection(target.dataset.section); break;
    case 'switch-profile': selectProfile(profile); break;
    case 'show-editor': mobileView = 'editor'; render(); break;
    case 'show-preview': mobileView = 'preview'; render(); break;
    case 'preview-draft': previewMode = 'draft'; render(); break;
    case 'preview-published': previewMode = 'published'; render(); break;
    case 'close-dialog': dialog.close(); break;
    case 'add': openEntity(section); break;
    case 'edit': openEntity(section, number); break;
    case 'remove-entity': removeEntity(number); break;
    case 'move-up': moveRule(number, -1); break;
    case 'move-down': moveRule(number, 1); break;
    case 'bulk': modal('批量添加规则', `<label class="field"><span>每行一条路由规则，按粘贴顺序加入。</span><textarea name="rules" rows="7" required spellcheck="false" class="mono" placeholder="DOMAIN-SUFFIX,example.com,DIRECT"></textarea></label>`, form => {
      const added = form.elements.rules.value.split('\n').map(line => line.trim()).filter(Boolean);
      if (!added.length) throw new Error('请填写至少一条规则。');
      const rules = [...(active().draft.rules ?? [])];
      insertRules(rules, added);
      const saved = saveDraft({ ...active().draft, rules });
      dialog.close();
      render();
      if (saved) notify('已添加 ' + added.length + ' 条规则');
    }, { submitLabel: '添加到草稿' }); break;
    case 'new-profile': openNewProfile(); break;
    case 'manage-profile': manageProfile(); break;
    case 'duplicate-profile': {
      const source = active();
      const copied = newProfile(source.name + '（副本）', source.target, structuredClone(source.draft));
      state.profiles.push(copied);
      state.selected = copied.id;
      const saved = persist();
      dialog.close();
      previewMode = 'draft';
      showSection('proxy-groups');
      if (saved) notify('已复制配置');
      break;
    }
    case 'remove-profile': confirmRemoveProfile(); break;
    case 'validate': validationDialog(); break;
    case 'locate-issue': {
      dialog.close();
      const key = target.dataset.section;
      const name = target.dataset.name;
      showSection(key);
      const row = listFor(active().draft, key).find(row => (key === 'rules' ? ruleLabel(row) : row.name) === name);
      if (row) document.querySelector(`#row-${row.index} button[data-action="edit"]`)?.focus();
      else document.getElementById('field-' + name)?.focus();
      break;
    }
    case 'download': download(); break;
    case 'copy-yaml': copy(toYAML(previewMode === 'published' && active().published ? active().published.config : active().draft), '已复制 YAML'); break;
    case 'publish': publish(); break;
    case 'links': linksDialog(); break;
    case 'copy-link': copy(linkURL(token), '已复制示例地址'); break;
    case 'new-link': modal('新建订阅链接', `<label class="field"><span>链接名称</span><input name="label" required maxlength="40" placeholder="例如：客厅路由器"></label>`, form => {
      const label = form.elements.label.value.trim();
      if (!label) throw new Error('请填写链接名称。');
      active().links.push({ token: uuid(), label });
      persist();
      linksDialog();
    }, { submitLabel: '生成示例地址' }); break;
    case 'revoke': modal('吊销订阅链接', '<p>确认从配置中移除此示例链接？</p>', () => {
      active().links = active().links.filter(link => link.token !== token);
      persist();
      linksDialog();
    }, { submitLabel: '确认吊销' }); break;
    case 'account': modal('当前账户', `<p>${escape(state.account)}</p><p class="muted">原型在此浏览器保存演示配置，支持体验登录、注册和退出。</p>`, null, { footer: button('退出登录', 'logout', { variant: 'danger', symbol: 'logout' }) }); break;
    case 'toggle-auth': authMode = authMode === 'login' ? 'register' : 'login'; render(); break;
    case 'logout':
      state.account = null;
      authMode = 'login';
      persist();
      dialog.close();
      render();
      break;
  }
});

document.addEventListener('submit', async event => {
  const form = event.target;
  event.preventDefault();
  if (form.id === 'dialog-form' && submitDialog) {
    const submit = form.querySelector('button[type="submit"]');
    if (submit) submit.disabled = true;
    try {
      await submitDialog(form);
    } catch (error) {
      const feedback = form.querySelector('#dialog-error');
      feedback.textContent = error.message;
      feedback.hidden = false;
      feedback.focus();
    } finally {
      if (submit) submit.disabled = false;
    }
  }
  if (form.id === 'settings-form') {
    try {
      const values = readFields(form, settingFields[section]);
      const saved = saveDraft(applySettings(active().draft, section, values));
      render();
      document.querySelector('#settings-form button[type="submit"]')?.focus();
      if (saved) notify('已保存到草稿');
    } catch (error) {
      const feedback = form.querySelector('#settings-error');
      feedback.textContent = error.message;
      feedback.hidden = false;
    }
  }
  if (form.id === 'auth-form') {
    const username = form.elements.username.value.trim();
    if (!username) {
      form.elements.username.setCustomValidity('请填写用户名。');
      form.elements.username.reportValidity();
      form.elements.username.addEventListener('input', () => form.elements.username.setCustomValidity(''), { once: true });
      return;
    }
    state.account = username;
    persist();
    render();
  }
});

render();
