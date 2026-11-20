import { basicKeys, entityKeys, isRecord, names, omit, parseYAML, policies, ruleTypes, yamlText } from './config.ts';
import type { Config, EntityKey, SettingsKey } from './config.ts';

export type InputValue = string | string[];
type FieldKind = 'text' | 'password' | 'url' | 'number' | 'select' | 'bool' | 'lines' | 'yaml' | 'refs';

export interface Field {
  key: string;
  label: string;
  kind: FieldKind;
  options?: [string, string][];
  max?: number;
  min?: number;
  required?: boolean;
  hint?: string;
  placeholder?: string;
  sensitive?: boolean;
  searchable?: boolean;
  path?: [string, string];
}

const choose = (items: string[]): [string, string][] => items.map(item => [item, item]);
const field = (key: string, label: string, kind: FieldKind = 'text', rest: Partial<Field> = {}): Field => ({ key, label, kind, ...rest });
const select = (key: string, label: string, options: string[], rest?: Partial<Field>) =>
  field(key, label, 'select', { options: choose(options), ...rest });
const refs = (key: string, label: string, options: string[], hint?: string) =>
  field(key, label, 'refs', { options: choose(options), hint });
const url = (label: string, required = false, sensitive = false) => field('url', label, 'url', { required, sensitive });
const interval = (label = '更新间隔（秒）') => field('interval', label, 'number', { min: 1 });
const path = field('path', '缓存 / 本地路径', 'text', { placeholder: './provider/example.yaml' });

const generalFields = [
  field('mixed-port', '混合端口', 'number', { max: 65535 }),
  field('port', 'HTTP 端口', 'number', { max: 65535 }),
  field('socks-port', 'SOCKS 端口', 'number', { max: 65535 }),
  field('redir-port', '透明代理端口', 'number', { max: 65535 }),
  select('mode', '运行模式', ['rule', 'global', 'direct']),
  select('log-level', '日志等级', ['silent', 'error', 'warning', 'info', 'debug']),
  field('external-controller', '控制器地址', 'text', { placeholder: '127.0.0.1:9090' }),
  field('allow-lan', '允许局域网连接', 'bool'),
  field('ipv6', '启用 IPv6', 'bool'),
];

const dnsStandard = [
  field('enable', '启用 DNS', 'bool'),
  select('enhanced-mode', '增强模式', ['fake-ip', 'redir-host']),
  field('listen', '监听地址', 'text', { placeholder: '127.0.0.1:1053' }),
  field('fake-ip-range', 'Fake-IP 网段', 'text', { placeholder: '198.18.0.1/16' }),
  field('default-nameserver', '默认解析器', 'lines', { hint: '一行填写一个地址。' }),
  field('nameserver', '解析器', 'lines'),
  field('fake-ip-filter', 'Fake-IP 过滤', 'lines', { searchable: true }),
  field('nameserver-policy', '域名解析策略', 'yaml', { hint: 'YAML 映射，每个域名可填一个解析器或解析器列表。' }),
  field('use-hosts', '使用 Hosts', 'bool'),
  field('ipv6', '解析 IPv6', 'bool'),
  field('fallback', '备用解析器', 'lines'),
  field('proxy-server-nameserver', '代理服务器域名解析器', 'lines'),
  field('respect-rules', 'DNS 连接遵循路由规则', 'bool'),
];

export const settingsFields: Record<SettingsKey, Field[]> = {
  basics: generalFields,
  dns: [...dnsStandard, field('dns-extra', '其他 DNS 字段（YAML）', 'yaml', { hint: '可填写 fallback-filter、fake-ip-filter-mode 等字段。' })],
  advanced: [field('advanced', '扩展 YAML', 'yaml')],
};

const name = field('name', '名称', 'text', { required: true, max: 80 });
const sourceType = select('type', '来源类型', ['http', 'file', 'inline'], { required: true });
const extras = field('extras', '其他字段（YAML）', 'yaml', { hint: '可填写传输参数及其他 Mihomo 字段。' });
const password = field('password', '密码', 'password');
const udp = field('udp', '启用 UDP', 'bool');
const tls = field('tls', '启用 TLS', 'bool');
const sni = field('sni', 'SNI');
const servername = field('servername', '服务器名称（SNI）');
const network = select('network', '传输方式', ['tcp', 'ws', 'http', 'h2', 'grpc']);
const uuid = field('uuid', 'UUID', 'text', { required: true });
const protocols: Record<string, Field[]> = {
  socks5: [field('username', '用户名'), password, udp, tls],
  http: [field('username', '用户名'), password, tls, sni],
  ss: [select('cipher', '加密方式', ['aes-128-gcm', 'aes-256-gcm', 'chacha20-ietf-poly1305', '2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305'], { required: true }), { ...password, required: true }, udp],
  vmess: [uuid, field('alterId', '额外 ID', 'number'), select('cipher', '加密方式', ['auto', 'aes-128-gcm', 'chacha20-poly1305', 'none', 'zero']), udp, tls, servername, network],
  vless: [uuid, select('flow', '流控', ['xtls-rprx-vision']), udp, tls, servername, network],
  trojan: [{ ...password, required: true }, sni, udp, network],
  hysteria2: [{ ...password, required: true }, sni, select('obfs', '混淆方式', ['salamander']), field('obfs-password', '混淆密码', 'password'), field('ports', '跳跃端口')],
};

export const newEntry: Record<EntityKey, Config> = {
  'proxy-providers': { type: 'http', interval: 3600 },
  proxies: { type: 'socks5', port: 1080 },
  'proxy-groups': { type: 'select', proxies: ['DIRECT'] },
  'rule-providers': { type: 'http', behavior: 'classical', interval: 86400 },
  rules: { type: 'DOMAIN-SUFFIX', target: 'DIRECT' },
};

export function entityFields(config: Config, key: EntityKey, type: string, editingName?: string): Field[] {
  const endpoints = policies(config, editingName);
  if (key === 'proxies') {
    return [
      name,
      select('type', '协议', Object.keys(protocols), { required: true }),
      field('server', '服务器', 'text', { required: true }),
      field('port', '服务器端口', 'number', { min: 1, max: 65535, required: type !== 'hysteria2' }),
      ...(Object.hasOwn(protocols, type) ? protocols[type] : []),
      select('dialer-proxy', '拨号出口', endpoints, { hint: '选择用于建立该节点连接的节点或代理组。' }),
      extras,
    ];
  }
  if (key === 'proxy-groups') {
    return [
      name,
      select('type', '类型', ['select', 'url-test', 'fallback', 'load-balance'], { required: true }),
      refs('proxies', '显式成员', endpoints, '使用上下箭头可调整 proxies 的顺序。'),
      refs('use', '订阅源', names(config, 'proxy-providers'), '按此顺序写入 use。'),
      field('filter', '节点筛选表达式'),
      field('exclude-filter', '排除表达式'),
      ...(type === 'select' ? [] : [url('测速地址'), interval('测速间隔（秒）')]),
      ...(type === 'url-test' ? [field('tolerance', '容差（毫秒）', 'number')] : []),
      ...(type === 'load-balance' ? [select('strategy', '均衡策略', ['consistent-hashing', 'round-robin', 'sticky-sessions'])] : []),
      extras,
    ];
  }
  if (key === 'proxy-providers') {
    return [
      name, sourceType,
      ...(type === 'http' ? [url('原始订阅地址', true, true), path, interval(), select('proxy', '下载使用的策略', endpoints)] : []),
      ...(type === 'file' ? [{ ...path, required: true }] : []),
      ...(type === 'inline' ? [field('inline-nodes', '节点内容（YAML）', 'yaml', { hint: '用 payload 字段填写节点列表。', required: true })] : []),
      field('filter', '节点筛选表达式'), field('exclude-filter', '排除表达式'),
      field('health-enabled', '启用健康检查', 'bool', { path: ['health-check', 'enable'] }),
      field('health-interval', '健康检查间隔（秒）', 'number', { min: 1, path: ['health-check', 'interval'] }),
      field('health-url', '健康检查地址', 'url', { path: ['health-check', 'url'] }), extras,
    ];
  }
  if (key === 'rule-providers') {
    return [
      name, sourceType, select('behavior', '匹配类型', ['domain', 'ipcidr', 'classical'], { required: true }),
      ...(type === 'inline' ? [field('payload', '规则内容', 'lines', { required: true, hint: '一行填写一条规则。' })] : [
        select('format', '文件格式', ['yaml', 'text', 'mrs'], { placeholder: '默认 YAML' }),
        ...(type === 'http' ? [url('规则集地址', true, true), interval(), select('proxy', '下载使用的策略', endpoints)] : []),
        { ...path, required: type === 'file' },
      ]), extras,
    ];
  }
  const typeField = select('type', '规则类型', ruleTypes, { options: [...choose(ruleTypes), ['RAW', '原始规则']], required: true });
  if (type === 'RAW') return [typeField, field('raw', '原始规则', 'text', { required: true })];
  return [
    typeField,
    ...(type === 'RULE-SET' ? [select('content', '规则集', names(config, 'rule-providers'), { required: true })]
      : type === 'MATCH' ? [] : [field('content', '匹配内容', 'text', { required: true })]),
    select('target', '目标策略', endpoints, { required: true }),
    ...(['GEOIP', 'IP-CIDR', 'IP-CIDR6', 'RULE-SET'].includes(type) ? [field('no-resolve', '不触发 DNS 解析', 'bool')] : []),
  ];
}

const dnsKeys = dnsStandard.map(spec => spec.key);
const managed = [...basicKeys, 'dns', ...entityKeys];

export function settingsValues(config: Config, key: SettingsKey): Config {
  if (key === 'basics') return config;
  if (key === 'dns') {
    const dns = isRecord(config.dns) ? config.dns : {};
    return { ...dns, 'dns-extra': omit(dns, dnsKeys) };
  }
  return { advanced: omit(config, managed) };
}

export function applySettings(config: Config, key: SettingsKey, values: Config) {
  if (key === 'basics') return { ...omit(config, basicKeys), ...values };
  if (key === 'dns') {
    const extra = isRecord(values['dns-extra']) ? values['dns-extra'] : {};
    if (Object.keys(extra).some(key => dnsKeys.includes(key))) throw new Error('请在对应的 DNS 表单字段修改该值。');
    return { ...config, dns: { ...omit(values, ['dns-extra']), ...extra } };
  }
  const extra = isRecord(values.advanced) ? values.advanced : {};
  if (Object.keys(extra).some(key => managed.includes(key))) throw new Error('请在对应模块修改常规设置、DNS、代理或路由规则。');
  return { ...Object.fromEntries(Object.entries(config).filter(([key]) => managed.includes(key))), ...extra };
}

export function entityValues(original: Config, fields: Field[], rawRule: boolean, originalFields = fields): Config {
  if (rawRule) return original;
  const managed = [...fields, ...originalFields].filter(field => !field.path)
    .map(field => field.key === 'inline-nodes' ? 'payload' : field.key);
  const extra = omit(original, managed);
  const nested: Config = {};
  for (const spec of fields) {
    if (!spec.path) continue;
    const [parent, child] = spec.path;
    const mapping = extra[parent];
    if (!isRecord(mapping) || !Object.hasOwn(mapping, child)) continue;
    nested[spec.key] = mapping[child];
    const rest = omit(mapping, [child]);
    if (Object.keys(rest).length) extra[parent] = rest;
    else delete extra[parent];
  }
  return {
    ...original, ...nested, ...('payload' in original ? { 'inline-nodes': { payload: original.payload } } : {}), extras: extra,
  };
}

export function applyEntity(values: Config): Config {
  const extra = isRecord(values.extras) ? values.extras : {};
  const inline = isRecord(values['inline-nodes']) ? values['inline-nodes'] : {};
  if (values.type === 'inline' && values['inline-nodes'] && !Array.isArray(inline.payload)) throw new Error('节点内容需要包含 payload 列表。');
  const health = isRecord(extra['health-check']) ? { ...extra['health-check'] } : {};
  const healthKeys = { 'health-enabled': 'enable', 'health-interval': 'interval', 'health-url': 'url' };
  for (const [key, property] of Object.entries(healthKeys)) if (Object.hasOwn(values, key)) health[property] = values[key];
  return {
    ...extra, ...omit(values, ['extras', 'inline-nodes', ...Object.keys(healthKeys)]),
    ...(Object.hasOwn(inline, 'payload') ? { payload: inline.payload } : {}),
    ...(Object.keys(health).length ? { 'health-check': health } : {}),
  };
}

export function displayValue(spec: Field, value: unknown): InputValue {
  if (spec.kind === 'refs') return Array.isArray(value) ? value.map(String) : [];
  if (value === undefined || value === null) return '';
  if (spec.kind === 'yaml') return isRecord(value) && Object.keys(value).length === 0 ? '' : yamlText(value).trimEnd();
  if (spec.kind === 'lines') return Array.isArray(value) ? value.join('\n') : String(value);
  return String(value);
}

export function formValues(fields: Field[], source: Config): Record<string, InputValue> {
  return Object.fromEntries(fields.map(spec => [spec.key, displayValue(spec, source[spec.key])]));
}

export function readValues(fields: Field[], form: Record<string, InputValue>, source: Config): Config {
  const entries: [string, unknown][] = [];
  for (const spec of fields) {
    const value = form[spec.key];
    const original = source[spec.key];
    if (value === undefined) continue;
    if (Object.hasOwn(source, spec.key) && JSON.stringify(value) === JSON.stringify(displayValue(spec, original))) {
      entries.push([spec.key, original]);
      continue;
    }
    if (Array.isArray(value)) {
      if (value.length || Array.isArray(original)) entries.push([spec.key, value]);
      continue;
    }
    const trimmed = value.trim();
    if (!trimmed && spec.kind !== 'password') continue;
    if (spec.kind === 'password' && value === '') continue;
    try {
      if (spec.kind === 'number') {
        const numeric = Number(trimmed);
        if (!Number.isInteger(numeric) || numeric < (spec.min ?? 0) || numeric > (spec.max ?? Infinity)) throw new Error('需要范围内的整数。');
        entries.push([spec.key, numeric]);
      } else if (spec.kind === 'bool') entries.push([spec.key, trimmed === 'true']);
      else if (spec.kind === 'lines') entries.push([spec.key, trimmed.split('\n').map(line => line.trim()).filter(Boolean)]);
      else if (spec.kind === 'yaml') entries.push([spec.key, parseYAML(trimmed)]);
      else entries.push([spec.key, spec.kind === 'password' ? value : trimmed]);
    } catch (error) {
      throw new Error(spec.label + '：' + (error instanceof Error ? error.message : String(error)));
    }
  }
  return Object.fromEntries(entries);
}
