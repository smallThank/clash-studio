import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseYAML } from '../src/config.ts';
import { yamlPreviewText } from '../src/yaml-preview.ts';

test('shows emoji and variation selectors without changing YAML values or layout', () => {
  const source = String.raw`# \U0001F680 stays in the comment
proxy-groups:
  - name: "\U0001F680 节点选择"
    proxies:
      - "\U0001F3AF 全球直连" # keep this comment
      - "\u2764\uFE0F 收藏"
      - "\U0001F469\u200D\U0001F4BB 工作"
      - "\uD83D\uDE00 表情"
aliases:
  "\U0001F431": "猫"
`;
  const preview = yamlPreviewText(source);
  assert.match(preview, /name: "🚀 节点选择"/u);
  assert.match(preview, /"🎯 全球直连" # keep this comment/u);
  assert.match(preview, /"❤️ 收藏"/u);
  assert.match(preview, /"👩‍💻 工作"/u);
  assert.match(preview, /"😀 表情"/u);
  assert.match(preview, /  "🐱": "猫"/u);
  assert.ok(preview.startsWith(String.raw`# \U0001F680 stays in the comment`));
  assert.deepEqual(parseYAML(preview), parseYAML(source));
  assert.equal(preview.split('\n').length, source.split('\n').length);
});

test('leaves literal escapes, syntax escapes and invisible controls intact', () => {
  const source = String.raw`plain: \U0001F680
single: '\U0001F680'
block: |
  "\U0001F680"
backslash: "\\U0001F680"
controls: "\u0000\u2028\u2029\u202E"
syntax: "\u0022\u005C\u0023"
`;
  assert.equal(yamlPreviewText(source), source);
  assert.equal(yamlPreviewText('unclosed: "\\U0001F680'), 'unclosed: "\\U0001F680');
});
