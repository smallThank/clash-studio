import { parseDocument, stringify } from 'yaml';
import type { Patch, Rename, Target } from './api.ts';

export type Config = Record<string, unknown>;
export type SettingsKey = 'basics' | 'dns' | 'advanced';
export type EntityKey = 'proxy-providers' | 'proxies' | 'proxy-groups' | 'rule-providers' | 'rules';
export type ModuleKey = SettingsKey | EntityKey;

interface Module {
  id: ModuleKey;
  label: string;
  icon: string;
  group: string;
  detail: string;
  add?: string;
}

export const modules: Module[] = [
  { id: 'basics', label: '常规设置', icon: 'sliders', group: '基础', detail: '配置监听端口、运行模式与局域网访问。' },
  { id: 'dns', label: 'DNS', icon: 'globe', group: '基础', detail: '管理解析器、域名解析策略与 Fake-IP 过滤。' },
  { id: 'proxy-providers', label: '订阅源', icon: 'rss', group: '代理', detail: '添加订阅源由客户端定期拉取，也可下载订阅节点写入当前配置。', add: '添加订阅源' },
  { id: 'proxies', label: '代理节点', icon: 'server', group: '代理', detail: '添加独立的代理节点，并设置可选的拨号出口。', add: '新建节点' },
  { id: 'proxy-groups', label: '代理组', icon: 'layers', group: '代理', detail: '组合节点、其他代理组或订阅源，为规则指定流量出口。', add: '新建代理组' },
  { id: 'rule-providers', label: '规则集', icon: 'box', group: '分流', detail: '管理可被 RULE-SET 引用的规则资源及下载策略。', add: '新建规则集' },
  { id: 'rules', label: '路由规则', icon: 'route', group: '分流', detail: '从上到下匹配。可调整优先级，将 MATCH 兜底放在末尾。', add: '新建规则' },
  { id: 'advanced', label: '扩展配置', icon: 'code', group: '分流', detail: '编辑随最终 YAML 一起导出的 TUN、Sniffer、Hosts 等字段。' },
];

export const basicKeys = ['mixed-port', 'port', 'socks-port', 'redir-port', 'allow-lan', 'mode', 'log-level', 'ipv6', 'external-controller'];
export const builtinPolicies = ['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'COMPATIBLE', 'GLOBAL'];
export const ruleTypes = ['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'GEOSITE', 'GEOIP', 'IP-CIDR', 'IP-CIDR6', 'RULE-SET', 'MATCH'];
export const targets: Record<Target, string> = { openclash: 'OpenClash', verge: 'Clash Verge Rev', both: '两端通用' };
export const yamlText = (config: unknown) => stringify(config, { lineWidth: 0 });

export function isRecord(value: unknown): value is Config {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

export function parseYAML(value: string): Config {
  const doc = parseDocument(value, { uniqueKeys: true, strict: true, merge: true });
  if (doc.errors.length) throw new Error(doc.errors[0].message);
  const result: unknown = doc.toJS({ maxAliasCount: 1000 });
  if (!isRecord(result)) throw new Error('需要一个 YAML 键值映射。');
  return result;
}

export const omit = (value: Config, keys: Iterable<string>): Config => {
  const removed = new Set(keys);
  return Object.fromEntries(Object.entries(value).filter(([key]) => !removed.has(key)));
};

export const entityKeys = new Set<ModuleKey>(modules.filter(module => module.add).map(module => module.id));
export function isEntityKey(key: ModuleKey): key is EntityKey {
  return entityKeys.has(key);
}

export function listFor(config: Config, key: EntityKey) {
  const value = config[key];
  if (Array.isArray(value)) {
    return value.map((item: unknown, index) => ({
      index, name: isRecord(item) ? String(item.name ?? '') : String(item), value: item,
    }));
  }
  if (isRecord(value)) {
    return Object.entries(value).map(([name, value], index) => ({ index, name, value }));
  }
  return [];
}

export function names(config: Config, key: EntityKey) {
  return listFor(config, key).map(row => row.name).filter(Boolean);
}

export function policies(config: Config, editingName?: string) {
  return [...new Set([...names(config, 'proxies'), ...names(config, 'proxy-groups'), ...builtinPolicies])]
    .filter(name => name !== editingName);
}

export interface Rule {
  type: string;
  target?: string;
  content?: string;
  'no-resolve'?: boolean;
  raw?: string;
}

export function parseRule(value: unknown): Rule {
  const raw = String(value);
  const [type, second, third, ...options] = raw.split(',').map(part => part.trim());
  if (!ruleTypes.includes(type)) return { type: 'RAW', raw };
  if (type === 'MATCH') return second && third === undefined ? { type, target: second } : { type: 'RAW', raw };
  if (!second || !third || options.some(option => option !== 'no-resolve') || options.length > 1) return { type: 'RAW', raw };
  return { type, content: second, target: third, 'no-resolve': options.includes('no-resolve') };
}

export function formatRule(data: Config): string {
  if (data.type === 'RAW') return String(data.raw ?? '');
  if (data.type === 'MATCH') return ['MATCH', data.target].join(',');
  return [data.type, data.content, data.target, ...(data['no-resolve'] ? ['no-resolve'] : [])].join(',');
}

export function insertRules(rules: string[], added: string[]) {
  const match = rules.findIndex(rule => parseRule(rule).type === 'MATCH');
  const ordinary = added.filter(rule => parseRule(rule).type !== 'MATCH');
  const fallback = added.filter(rule => parseRule(rule).type === 'MATCH');
  if (match < 0) return [...rules, ...ordinary, ...fallback];
  return [...rules.slice(0, match), ...ordinary, ...rules.slice(match), ...fallback];
}

export function moveItem<T>(list: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return list;
  const copy = [...list];
  const [item] = copy.splice(from, 1);
  copy.splice(to, 0, item);
  return copy;
}

export function withEntity(config: Config, key: EntityKey, index: number | null, data: Config | string | null): Config {
  const rows = listFor(config, key);
  if (key === 'rules') {
    const rules = rows.map(row => String(row.value));
    if (index === null) return { ...config, rules: insertRules(rules, [String(data)]) };
    if (data === null) rules.splice(index, 1);
    else rules[index] = String(data);
    return { ...config, rules };
  }
  const name = isRecord(data) ? String(data.name ?? '') : '';
  if (data && !name.trim()) throw new Error('请填写名称。');
  if (name.includes(',')) throw new Error('名称中不能包含逗号。');
  if (data && rows.some(row => row.index !== index && row.name === name)) throw new Error('此名称已存在。');
  const mapping = key.endsWith('-providers');
  if (mapping) {
    const entries = rows.map(row => [row.name, row.value]);
    if (data === null && index !== null) entries.splice(index, 1);
    else if (isRecord(data)) {
      const entry = [name, omit(data, ['name'])];
      if (index === null) entries.push(entry);
      else entries[index] = entry;
    }
    return { ...config, [key]: Object.fromEntries(entries) };
  }
  const values = rows.map(row => row.value);
  if (data === null && index !== null) values.splice(index, 1);
  else if (index === null) values.push(data);
  else values[index] = data;
  return { ...config, [key]: values };
}

export function draftPatch(previous: Config, next: Config, rename?: Rename): Patch {
  const changes = Object.fromEntries(Object.entries(next).filter(([key, value]) =>
    !Object.hasOwn(previous, key) || JSON.stringify(value) !== JSON.stringify(previous[key]),
  ));
  const remove = Object.keys(previous).filter(key => !Object.hasOwn(next, key));
  return { changes, remove, ...(rename ? { rename } : {}) };
}

export function configDiff(before: string, after: string) {
  const left = parseYAML(before);
  const right = parseYAML(after);
  const patch = draftPatch(left, right);
  return [...Object.keys(patch.changes), ...patch.remove].map(key => ({
    key,
    before: Object.hasOwn(left, key) ? yamlText({ [key]: left[key] }) : '',
    after: Object.hasOwn(right, key) ? yamlText({ [key]: right[key] }) : '',
  }));
}
