import assert from 'node:assert/strict';
import { test } from 'node:test';
import { configDiff, draftPatch, formatRule, insertRules, moveItem, parseRule, parseYAML, withEntity, yamlText } from '../src/config.ts';
import { applyEntity, applySettings, entityFields, entityValues, formValues, readValues, settingsFields, settingsValues } from '../src/fields.ts';

const sample = parseYAML(`
mixed-port: 0
allow-lan: false
dns:
  enable: true
  listen: null
  nameserver:
    - 1.1.1.1
  nameserver-policy:
    +.sample.example: [1.1.1.1, 9.9.9.9]
  fake-ip-filter: []
  fallback-filter:
    geoip: false
proxy-providers:
  upstream:
    type: http
    url: https://example.invalid/proxy
    health-check:
      enable: false
      interval: 300
      timeout: 5000
proxies:
  - name: First
    type: socks5
    server: first.example.invalid
    port: 1080
  - name: Last
    type: socks5
    server: last.example.invalid
    port: 1080
    dialer-proxy: First
proxy-groups:
  - name: Select
    type: fallback
    proxies: [First, Last, DIRECT]
    use: [upstream]
rule-providers:
  sites:
    type: inline
    behavior: domain
    payload: [+.sample.example]
rules:
  - RULE-SET,sites,Select,no-resolve
  - MATCH,DIRECT
tun:
  enable: false
`);

test('DNS field editing preserves omitted, null, empty, and unknown imported values', () => {
  const initial = settingsValues(sample, 'dns');
  const specs = settingsFields.dns;
  const form = formValues(specs, initial);
  form.nameserver = '8.8.8.8\n9.9.9.9';
  const edited = applySettings(sample, 'dns', readValues(specs, form, initial));
  assert.deepEqual(edited.dns, {
    enable: true, listen: null, nameserver: ['8.8.8.8', '9.9.9.9'],
    'nameserver-policy': { '+.sample.example': ['1.1.1.1', '9.9.9.9'] },
    'fake-ip-filter': [], 'fallback-filter': { geoip: false },
  });
  assert.equal(edited['mixed-port'], 0);
  assert.equal(edited['allow-lan'], false);
  assert.deepEqual(Object.keys(draftPatch(sample, edited).changes), ['dns']);
  assert.throws(() => applySettings(sample, 'dns', { 'dns-extra': { enable: true } }), /对应的 DNS/);
});

test('proxy-provider form preserves nested health-check values and masks its URL', () => {
  const original = (sample['proxy-providers'] as Record<string, Record<string, unknown>>).upstream;
  const specs = entityFields(sample, 'proxy-providers', 'http');
  const values = entityValues({ ...original, name: 'upstream' }, specs, false);
  const form = formValues(specs, values);
  form['health-interval'] = '301';
  const edited = applyEntity(readValues(specs, form, values));
  assert.deepEqual(edited['health-check'], { enable: false, interval: 301, timeout: 5000 });
  assert.equal(specs.find(spec => spec.key === 'url')?.sensitive, true);
});

test('switching providers drops inactive typed fields and keeps other advanced fields', () => {
  const original = {
    name: 'upstream', type: 'http', url: 'https://example.invalid/proxy', interval: 3600, path: './source.yaml',
    'health-check': { enable: true, timeout: 4000 },
    'exclude-filter': 'excluded', 'exclude-type': 'ss',
  };
  const originalFields = entityFields(sample, 'proxy-providers', 'http');
  const fields = entityFields(sample, 'proxy-providers', 'inline');
  const values = entityValues(original, fields, false, originalFields);
  const form = formValues(fields, values);
  form.type = 'inline';
  form['inline-nodes'] = 'payload:\n  - {name: Direct, type: direct}';
  const edited = applyEntity(readValues(fields, form, values));
  assert.equal(edited.type, 'inline');
  for (const key of ['path', 'url', 'interval']) assert.ok(!Object.hasOwn(edited, key));
  assert.deepEqual(edited['health-check'], { enable: true, timeout: 4000 });
  assert.deepEqual(edited.payload, [{ name: 'Direct', type: 'direct' }]);
  assert.equal(edited['exclude-type'], 'ss');
});

test('group member order, use, and node dialer references are sent independently', () => {
  const groups = sample['proxy-groups'] as Record<string, unknown>[];
  const specs = entityFields(sample, 'proxy-groups', 'fallback', 'Select');
  const initial = entityValues(groups[0], specs, false);
  const form = formValues(specs, initial);
  form.proxies = moveItem(form.proxies as string[], 1, 0);
  const config = withEntity(sample, 'proxy-groups', 0, applyEntity(readValues(specs, form, initial)));
  assert.deepEqual((config['proxy-groups'] as Record<string, unknown>[])[0].proxies, ['Last', 'First', 'DIRECT']);
  assert.deepEqual((config['proxy-groups'] as Record<string, unknown>[])[0].use, ['upstream']);
  const proxyFields = entityFields(sample, 'proxies', 'socks5', 'Last');
  const endpoints = proxyFields.find(field => field.key === 'dialer-proxy')?.options ?? [];
  assert.ok(endpoints.some(([value]) => value === 'First'));
  assert.ok(endpoints.some(([value]) => value === 'Select'));
  assert.ok(!endpoints.some(([value]) => value === 'Last'));
  const edited = withEntity(config, 'proxy-groups', 0, { ...groups[0], name: 'Renamed' });
  assert.deepEqual(draftPatch(sample, edited, { kind: 'proxy-groups', from: 'Select', to: 'Renamed' }).rename,
    { kind: 'proxy-groups', from: 'Select', to: 'Renamed' });
});

test('all promised node protocols expose authentication fields', () => {
  for (const protocol of ['ss', 'vmess', 'vless', 'trojan', 'hysteria2']) {
    const specs = entityFields(sample, 'proxies', protocol);
    assert.ok(specs.some(spec => spec.key === (['vmess', 'vless'].includes(protocol) ? 'uuid' : 'password') && spec.required));
  }
});

test('route operations preserve no-resolve and raw syntax and place additions above MATCH', () => {
  const raw = 'AND,((DOMAIN,sample.example),(NETWORK,tcp)),DIRECT';
  assert.deepEqual(parseRule(raw), { type: 'RAW', raw });
  assert.equal(formatRule({ ...parseRule('RULE-SET,sites,Select,no-resolve') }), 'RULE-SET,sites,Select,no-resolve');
  assert.deepEqual(insertRules(['MATCH,DIRECT'], [raw, 'DOMAIN,example.test,DIRECT']),
    [raw, 'DOMAIN,example.test,DIRECT', 'MATCH,DIRECT']);
});

test('version diff marks added, removed, and edited YAML sections', () => {
  const next = applySettings(sample, 'advanced', { advanced: { sniffer: { enable: true } } });
  const changes = configDiff(yamlText(sample), yamlText(next));
  assert.deepEqual(changes.map(change => change.key), ['sniffer', 'tun']);
  assert.ok(!changes[0].before && changes[0].after);
  assert.ok(changes[1].before && !changes[1].after);
  assert.throws(() => applySettings(sample, 'advanced', { advanced: { rules: [] } }), /对应模块/);
});
