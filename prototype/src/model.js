import { parseDocument, stringify } from 'yaml';

export const modules = [
  { id: 'basics', label: '常规设置', icon: 'sliders', group: '基础', title: '常规设置', detail: '配置监听端口、运行模式与局域网访问。' },
  { id: 'dns', label: 'DNS', icon: 'globe', group: '基础', title: 'DNS', detail: '管理解析器、域名解析策略与 Fake-IP 过滤。' },
  { id: 'proxy-providers', label: '订阅源', icon: 'rss', group: '代理', title: '订阅源', detail: '添加服务商订阅地址，供代理组通过 use 引用。', add: '添加订阅源' },
  { id: 'proxies', label: '代理节点', icon: 'server', group: '代理', title: '代理节点', detail: '添加独立的代理节点，并设置可选的拨号出口。', add: '新建节点' },
  { id: 'proxy-groups', label: '代理组', icon: 'layers', group: '代理', title: '代理组', detail: '组合节点、其他代理组或订阅源，为规则指定流量出口。', add: '新建代理组' },
  { id: 'rule-providers', label: '规则集', icon: 'box', group: '分流', title: '规则集', detail: '管理可被 RULE-SET 引用的规则资源及下载策略。', add: '新建规则集' },
  { id: 'rules', label: '路由规则', icon: 'route', group: '分流', title: '路由规则', detail: '从上到下匹配。可调整优先级，将 MATCH 兜底放在末尾。', add: '新建规则' },
  { id: 'advanced', label: '扩展配置', icon: 'code', group: '分流', title: '扩展配置', detail: '编辑随最终 YAML 一起导出的 TUN、Sniffer、Hosts 等字段。' },
];

export const basics = ['mixed-port', 'port', 'socks-port', 'redir-port', 'allow-lan', 'mode', 'log-level', 'ipv6', 'external-controller'];
export const builtins = ['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'COMPATIBLE', 'GLOBAL'];
export const listKeys = modules.filter(module => module.add).map(module => module.id);
export const isMapping = value => value !== null && typeof value === 'object' && !Array.isArray(value);
export const uuid = () => crypto.randomUUID();
export const toYAML = value => stringify(value, { lineWidth: 0 });

export function parseYAML(value) {
  const doc = parseDocument(value, { uniqueKeys: true, strict: true });
  if (doc.errors.length) throw new Error(doc.errors[0].message);
  const result = doc.toJS({ maxAliasCount: 20 });
  if (!isMapping(result)) throw new Error('需要一个 YAML 键值映射。');
  return result;
}

export function listFor(config, key) {
  const value = config[key];
  if (Array.isArray(value)) {
    return value.map((item, index) => ({
      index, name: item?.name ?? String(item), value: item,
    }));
  }
  if (isMapping(value)) {
    return Object.entries(value).map(([name, value], index) => ({ index, name, value }));
  }
  return [];
}

export function newProfile(name, target, draft = { 'mixed-port': 7890, mode: 'rule', rules: ['MATCH,DIRECT'] }) {
  return { id: uuid(), name, target, draft, savedAt: Date.now(), published: null, links: [] };
}

const testURL = 'https://www.gstatic.com/generate_204';
export function initialState() {
  const primary = newProfile('家庭网络', 'openclash', {
    'mixed-port': 7890,
    'allow-lan': false,
    mode: 'rule',
    'log-level': 'info',
    ipv6: false,
    'external-controller': '127.0.0.1:9090',
    dns: {
      enable: true, 'enhanced-mode': 'fake-ip', 'fake-ip-range': '198.18.0.1/16',
      'default-nameserver': ['223.5.5.5'],
      nameserver: ['https://dns.alidns.com/dns-query'],
      'fake-ip-filter': ['+.lan', '+.local'],
      'nameserver-policy': { '+.example.com': ['https://dns.alidns.com/dns-query'] },
    },
    'proxy-providers': {
      '示例订阅': { type: 'http', url: 'https://provider.example.invalid/nodes.yaml', path: './proxy_provider/demo.yaml', interval: 3600,
        'health-check': { enable: true, interval: 300, url: testURL } },
    },
    proxies: [
      { name: '香港 · 手工节点', type: 'socks5', server: 'hk.example.invalid', port: 1080 },
      { name: '东京 · 手工节点', type: 'socks5', server: 'jp.example.invalid', port: 1080 },
    ],
    'proxy-groups': [
      { name: '节点选择', type: 'select', proxies: ['自动选择', '故障切换', 'DIRECT'], use: ['示例订阅'] },
      { name: '自动选择', type: 'url-test', proxies: ['香港 · 手工节点', '东京 · 手工节点'], use: ['示例订阅'], url: testURL, interval: 300, tolerance: 50 },
      { name: '故障切换', type: 'fallback', proxies: ['香港 · 手工节点', '东京 · 手工节点'], url: testURL, interval: 300 },
      { name: '流媒体', type: 'select', proxies: ['节点选择', '自动选择', 'DIRECT'] },
    ],
    'rule-providers': {
      Streaming: { type: 'http', behavior: 'classical', url: 'https://rules.example.invalid/streaming.yaml', path: './rule_provider/streaming.yaml', interval: 86400, proxy: '节点选择' },
      Advertising: { type: 'inline', behavior: 'domain', payload: ['+.ads.example.invalid'] },
    },
    rules: ['RULE-SET,Advertising,REJECT', 'RULE-SET,Streaming,流媒体', 'DOMAIN-SUFFIX,example.com,DIRECT', 'GEOIP,CN,DIRECT,no-resolve', 'MATCH,节点选择'],
    sniffer: { enable: true },
  });
  const laptop = newProfile('桌面设备', 'verge');
  return { profiles: [primary, laptop], selected: primary.id, account: '体验用户' };
}

export const editableRules = ['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'GEOSITE', 'GEOIP', 'IP-CIDR', 'IP-CIDR6', 'RULE-SET', 'MATCH'];

export function parseRule(value) {
  const [type, second, third, ...options] = String(value).split(',').map(token => token.trim());
  if (!editableRules.includes(type)) return { type: 'RAW', raw: value };
  if (type === 'MATCH') {
    if (!second || third !== undefined) return { type: 'RAW', raw: value };
    return { type, target: second };
  }
  if (options.some(option => option !== 'no-resolve') || options.length > 1) return { type: 'RAW', raw: value };
  return { type, content: second, target: third, 'no-resolve': options.includes('no-resolve') };
}

function validURL(value) {
  try {
    const url = new URL(value);
    return ['http:', 'https:'].includes(url.protocol);
  } catch {
    return false;
  }
}

export function validate(config) {
  const errors = [];
  const warnings = [];
  const error = (key, name, message) => errors.push({ key, name, message });
  const warning = (key, name, message) => warnings.push({ key, name, message });

  for (const key of listKeys) {
    const mapping = key.endsWith('-providers');
    if (config[key] !== undefined && !(mapping ? isMapping(config[key]) : Array.isArray(config[key]))) {
      error(key, key, mapping ? '此段需要 YAML 键值映射。' : '此段需要 YAML 列表。');
    }
  }
  for (const key of basics.filter(name => name.endsWith('port') || name === 'port')) {
    if (config[key] !== undefined && (!Number.isInteger(config[key]) || config[key] < 0 || config[key] > 65535)) {
      error('basics', key, '端口应为 0–65535 的整数。');
    }
  }
  if (config.dns !== undefined && !isMapping(config.dns)) error('dns', 'dns', '此段需要 YAML 键值映射。');

  const nodes = listFor(config, 'proxies');
  const groups = listFor(config, 'proxy-groups');
  const providers = listFor(config, 'proxy-providers');
  const ruleSets = listFor(config, 'rule-providers');
  const rules = listFor(config, 'rules');
  const names = new Set(builtins);
  const dependencies = new Map();
  const defined = new Set();
  const providerNames = new Set(providers.map(row => row.name));
  const ruleSetNames = new Set(ruleSets.map(row => row.name));

  for (const [rows, key] of [[nodes, 'proxies'], [groups, 'proxy-groups'], [providers, 'proxy-providers'], [ruleSets, 'rule-providers']]) {
    for (const { name, value } of rows) {
      if (!isMapping(value) || typeof value.type !== 'string' || !value.type) {
        error(key, name, '需要有效的 type 和配置字段。');
        continue;
      }
      if (!key.endsWith('-providers')) {
        if (typeof value.name !== 'string' || !value.name.trim()) error(key, name, '请填写名称。');
        if (names.has(name)) error(key, name, '名称已被使用或为内置策略名。');
        names.add(name);
        defined.add(name);
      } else if (value.type === 'http' && !validURL(value.url)) {
        error(key, name, 'HTTP 来源需要有效的 URL。');
      }
    }
  }

  for (const { name, value } of nodes) {
    if (value?.type === 'socks5' && (!value.server || !Number.isInteger(value.port) || value.port < 1 || value.port > 65535)) {
      error('proxies', name, 'SOCKS5 节点需要服务器和有效端口。');
    }
    if (value?.['dialer-proxy']) {
      const dialer = value['dialer-proxy'];
      if (!names.has(dialer)) error('proxies', name, '拨号出口不存在。');
      dependencies.set(name, { key: 'proxies', edges: [dialer] });
    }
  }
  for (const { name, value } of groups) {
    const members = Array.isArray(value?.proxies) ? value.proxies : [];
    const sources = Array.isArray(value?.use) ? value.use : [];
    if (value?.proxies !== undefined && !Array.isArray(value.proxies)) error('proxy-groups', name, 'proxies 需要列表。');
    if (value?.use !== undefined && !Array.isArray(value.use)) error('proxy-groups', name, 'use 需要列表。');
    if (!members.length && !sources.length && !value?.['include-all'] && !value?.['include-all-providers'] && !value?.['include-all-proxies']) {
      error('proxy-groups', name, '请至少添加一个成员或订阅源。');
    }
    for (const member of members) if (!names.has(member)) error('proxy-groups', name, '成员不存在：' + member);
    for (const source of sources) if (!providerNames.has(source)) error('proxy-groups', name, '订阅源不存在：' + source);
    dependencies.set(name, { key: 'proxy-groups', edges: members });
  }
  const visiting = new Set();
  const visited = new Set();
  function visit(name) {
    const dependency = dependencies.get(name);
    if (!defined.has(name) || !dependency || visited.has(name)) return;
    if (visiting.has(name)) {
      error(dependency.key, name, '节点或代理组形成了循环引用。');
      return;
    }
    visiting.add(name);
    dependency.edges.forEach(visit);
    visiting.delete(name);
    visited.add(name);
  }
  dependencies.forEach((_, name) => visit(name));

  for (const { name, value } of ruleSets) {
    if (value?.format === 'mrs' && !['domain', 'ipcidr'].includes(value.behavior)) {
      error('rule-providers', name, 'MRS 仅支持 domain 或 ipcidr。');
    }
    if (value?.proxy && !names.has(value.proxy)) error('rule-providers', name, '规则集下载出口不存在。');
  }
  for (const { index, value } of rules) {
    const name = '第 ' + (index + 1) + ' 条规则';
    if (typeof value !== 'string' || !value.trim()) {
      error('rules', name, '规则需要是非空字符串。');
      continue;
    }
    const { type, target, content } = parseRule(value);
    if (type === 'RAW') {
      warning('rules', name, '保留原始规则，需用目标客户端检查。');
      continue;
    }
    if (type !== 'MATCH' && !content) error('rules', name, '缺少匹配内容。');
    if (type === 'RULE-SET' && !ruleSetNames.has(content)) error('rules', name, '规则集不存在：' + content);
    if (!names.has(target)) error('rules', name, '目标策略不存在：' + (target || '未填写'));
    if (type === 'MATCH' && index < rules.length - 1) error('rules', name, 'MATCH 后面的规则无法命中。请将它移到末尾。');
  }
  return { errors, warnings };
}
