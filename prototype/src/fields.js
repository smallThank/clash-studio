import { basics, builtins, editableRules, listFor, toYAML, parseYAML, isMapping } from './model.js';

const choice = (values) => values.map(value => [value, value]);
const field = (key, label, kind = 'text', options = {}) => ({ key, label, kind, ...options });
const refField = (key, label, options, kind = 'select') => field(key, label, kind, { options: choice(options) });
const generalFields = [
  field('mixed-port', '混合端口', 'number', { max: 65535 }),
  field('port', 'HTTP 端口', 'number', { max: 65535 }),
  field('socks-port', 'SOCKS 端口', 'number', { max: 65535 }),
  field('redir-port', '透明代理端口', 'number', { max: 65535 }),
  field('mode', '运行模式', 'select', { options: choice(['rule', 'global', 'direct']) }),
  field('log-level', '日志等级', 'select', { options: choice(['silent', 'error', 'warning', 'info', 'debug']) }),
  field('external-controller', '控制器地址', 'text', { placeholder: '127.0.0.1:9090' }),
  field('allow-lan', '允许局域网连接', 'bool'),
  field('ipv6', '启用 IPv6', 'bool'),
];
const dnsFields = [
  field('enable', '启用 DNS', 'bool'),
  field('enhanced-mode', '增强模式', 'select', { options: choice(['fake-ip', 'redir-host']) }),
  field('listen', '监听地址', 'text', { placeholder: '127.0.0.1:1053' }),
  field('fake-ip-range', 'Fake-IP 网段', 'text', { placeholder: '198.18.0.1/16' }),
  field('default-nameserver', '默认解析器', 'lines', { hint: '一行填写一个地址。' }),
  field('nameserver', '解析器', 'lines'),
  field('fake-ip-filter', 'Fake-IP 过滤', 'lines'),
  field('nameserver-policy', '域名解析策略', 'yaml', { hint: 'YAML 映射，每个域名可填一个解析器或解析器列表。' }),
  field('use-hosts', '使用 Hosts', 'bool'),
];
const dnsExtra = field('dns-extra', '其他 DNS 字段（YAML）', 'yaml', { hint: '可填写 fallback、proxy-server-nameserver、respect-rules 等字段。' });

export const settingFields = { basics: generalFields, dns: [...dnsFields, dnsExtra], advanced: [field('advanced', '扩展 YAML', 'yaml')] };

const sourceType = field('type', '来源类型', 'select', { options: choice(['http', 'file', 'inline']), required: true });
const urlField = (label, required = false) => field('url', label, 'url', { required });
const pathField = field('path', '缓存 / 本地路径', 'text', { placeholder: './provider/example.yaml' });
const intervalField = (label = '更新间隔（秒）') => field('interval', label, 'number', { min: 1 });

export function entityFields(config, key, type, editingName) {
  const endpoints = [...listFor(config, 'proxies').map(row => row.name), ...listFor(config, 'proxy-groups').map(row => row.name), ...builtins];
  const targets = endpoints.filter(name => name !== editingName);
  const nameField = field('name', '名称', 'text', { required: true, max: 80 });
  const rawFields = field('extras', '其他字段（YAML）', 'yaml', { hint: '保留表单之外的协议、传输和高级字段。' });

  switch (key) {
    case 'proxy-providers':
      return [nameField, sourceType, ...(type === 'http' ? [urlField('原始订阅地址', true)] : []),
        ...(type === 'inline' ? [] : [pathField, intervalField()]),
        field('filter', '节点筛选表达式'),
        field('health-check', '健康检查（YAML）', 'yaml', { hint: '填写 enable、interval、url 等字段。' }), rawFields];
    case 'proxies':
      return [nameField, field('type', '协议', 'select', { options: choice(['socks5', 'http', 'ss', 'vmess', 'vless', 'trojan', 'hysteria2']), required: true }),
        field('server', '服务器', 'text', { required: true }),
        field('port', '服务器端口', 'number', { min: 1, max: 65535, required: true }),
        ...(type === 'http' || type === 'socks5' ? [field('username', '用户名'), field('password', '密码', 'password')] : []),
        refField('dialer-proxy', '拨号出口', targets), rawFields];
    case 'proxy-groups':
      return [nameField, field('type', '类型', 'select', { options: choice(['select', 'url-test', 'fallback', 'load-balance']), required: true }),
        refField('proxies', '显式成员', targets, 'refs'),
        refField('use', '订阅源', listFor(config, 'proxy-providers').map(row => row.name), 'refs'),
        field('filter', '节点筛选表达式'),
        ...(type === 'select' ? [] : [urlField('测速地址'), intervalField('测速间隔（秒）')]),
        ...(type === 'url-test' ? [field('tolerance', '容差（毫秒）', 'number')] : []), rawFields];
    case 'rule-providers':
      return [nameField, sourceType, field('behavior', '匹配类型', 'select', { options: choice(['domain', 'ipcidr', 'classical']), required: true }),
        ...(type === 'inline' ? [field('payload', '规则内容', 'lines', { required: true, hint: '一行填写一条规则。' })] : [
          field('format', '文件格式', 'select', { options: choice(['yaml', 'text', 'mrs']), placeholder: '默认 YAML' }),
          ...(type === 'http' ? [urlField('规则集地址', true), intervalField(), refField('proxy', '下载使用的策略', targets)] : []),
          { ...pathField, required: type === 'file' },
        ]), rawFields];
    case 'rules': {
      const fields = [field('type', '规则类型', 'select', { options: [...choice(editableRules), ['RAW', '原始规则']], required: true })];
      if (type === 'RAW') return [...fields, field('raw', '原始规则', 'text', { required: true })];
      if (type === 'RULE-SET') {
        fields.push({ ...refField('content', '规则集', listFor(config, 'rule-providers').map(row => row.name)), required: true });
      } else if (type !== 'MATCH') {
        fields.push(field('content', '匹配内容', 'text', { required: true }));
      }
      fields.push({ ...refField('target', '目标策略', endpoints), required: true });
      if (['GEOIP', 'IP-CIDR', 'IP-CIDR6', 'RULE-SET'].includes(type)) fields.push(field('no-resolve', '不触发 DNS 解析', 'bool'));
      return fields;
    }
    default: return [];
  }
}

export const startValues = {
  'proxy-providers': { type: 'http', interval: 3600 },
  proxies: { type: 'socks5', port: 1080 },
  'proxy-groups': { type: 'select', proxies: ['DIRECT'] },
  'rule-providers': { type: 'http', behavior: 'classical', interval: 86400 },
  rules: { type: 'DOMAIN-SUFFIX', target: 'DIRECT' },
};

const dnsKeys = dnsFields.map(spec => spec.key);
const standardKeys = [...basics, 'dns', 'proxy-providers', 'proxies', 'proxy-groups', 'rule-providers', 'rules'];

export function settingsValues(config, key) {
  if (key === 'basics') return config;
  if (key === 'dns') {
    const dns = isMapping(config.dns) ? config.dns : {};
    return { ...dns, 'dns-extra': omit(dns, dnsKeys) };
  }
  return { advanced: omit(config, standardKeys) };
}

export function applySettings(config, key, values) {
  if (key === 'basics') return { ...omit(config, basics), ...values };
  if (key === 'dns') {
    const extras = values['dns-extra'] ?? {};
    if (Object.keys(extras).some(name => dnsKeys.includes(name))) throw new Error('请在对应的 DNS 表单字段修改该值。');
    return { ...config, dns: { ...omit(values, ['dns-extra']), ...extras } };
  }
  const advanced = values.advanced ?? {};
  if (Object.keys(advanced).some(name => standardKeys.includes(name))) throw new Error('请在对应模块修改常规设置、DNS、代理或路由规则。');
  const current = settingsValues(config, key);
  return { ...omit(config, Object.keys(current.advanced)), ...advanced };
}

export const omit = (object, keys) => Object.fromEntries(Object.entries(object).filter(([key]) => !keys.includes(key)));

export function readField(form, spec) {
  if (spec.kind === 'refs') return Array.from(form.querySelectorAll('[data-ref="' + spec.key + '"]:checked'), input => input.value);
  const input = form.elements[spec.key === 'name' ? 'entry-name' : spec.key];
  if (!input) return undefined;
  const value = input.value.trim();
  if (!value) return undefined;
  try {
    if (spec.kind === 'bool') return value === 'true';
    if (spec.kind === 'number') return Number(value);
    if (spec.kind === 'lines') return value.split('\n').map(item => item.trim()).filter(Boolean);
    if (spec.kind === 'yaml') return parseYAML(value);
    return spec.kind === 'password' ? input.value : value;
  } catch (error) {
    throw new Error(spec.label + '：' + error.message);
  }
}

export function readFields(form, specs) {
  return Object.fromEntries(specs.map(spec => [spec.key, readField(form, spec)]).filter(([, value]) => value !== undefined));
}

export function fieldString(spec, value) {
  if (value === undefined || value === null) return '';
  if (spec.kind === 'yaml') return isMapping(value) && Object.keys(value).length ? toYAML(value).trimEnd() : '';
  if (spec.kind === 'lines') return Array.isArray(value) ? value.join('\n') : String(value);
  return String(value);
}
