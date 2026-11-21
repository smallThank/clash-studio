async (page) => {
  const fixture = String.raw`mode: Rule
mixed-port: 7890
dns:
  enable: true
  nameserver: [1.1.1.1]
profile:
  emoji-fixture: "\U0001F680 \U0001F3AF \U0001F431"
proxies:
  - {name: 测试中继, type: socks5, server: 192.0.2.25, port: 1080}
  - {name: "\U0001F431 表情节点", type: socks5, server: 192.0.2.26, port: 1080}
proxy-groups:
  - {name: 节点选择, type: select, proxies: [测试中继, DIRECT]}
  - {name: "\U0001F3AF 表情分组", type: select, proxies: ["\U0001F431 表情节点", DIRECT]}
rules:
  - DOMAIN,example.org,DIRECT
  - MATCH,节点选择
`;
  const profile = '浏览器验收配置';
  const username = 'tester_' + Date.now();
  const password = 'synthetic-browser-password-87';
  const modal = () => page.getByRole('dialog');
  const button = (name, scope = page) => scope.getByRole('button', { name, exact: typeof name === 'string' });
  const field = (name, scope = page) => scope.getByLabel(name, { exact: true });
  const preview = () => page.getByRole('region', { name: '生成的 YAML' });
  const importPreview = () => field('导入 YAML 预览', modal());
  const verify = (value, message) => { if (!value) throw Error(message); };
  const shown = locator => locator.waitFor({ state: 'visible' });
  const section = name => button(new RegExp('^' + name + '(?:\\s|$)'), page.getByRole('navigation', { name: '配置模块' })).click();
  const finish = async name => { await button(name, modal()).click(); await modal().waitFor({ state: 'hidden' }); };
  const close = async () => { await button('关闭', modal()).last().click(); await modal().waitFor({ state: 'hidden' }); };
  const save = () => finish('保存到草稿');

  await page.setViewportSize({ width: 1440, height: 900 });
  page.setDefaultTimeout(10000);
  const browserErrors = [];
  page.on('pageerror', error => browserErrors.push(error.message));
  await button('创建账号').click();
  await field('用户名').fill(username);
  await field('密码').fill(password);
  await button('注册并进入').click();
  await button('新建配置', page.getByRole('main')).click();
  await field('配置名称', modal()).fill(profile);
  const probeRoute = '**/api/v1/imports/probe';
  await page.route(probeRoute, route => route.fulfill({
    json: { id: 'test-import', kind: 'config', host: 'upstream.example.invalid', yaml: fixture },
  }));
  await button('原始订阅地址', modal()).click();
  await field('完整配置订阅地址', modal()).fill('https://upstream.example.invalid/config.yaml');
  await button('预览导入', modal()).click();
  await shown(importPreview().getByText(/🚀 🎯 🐱/u));
  await page.unroute(probeRoute);

  await button('导入 YAML', modal()).click();
  await modal().locator('input[type=file]').setInputFiles({
    name: 'fixture.yaml', mimeType: 'application/yaml', buffer: Buffer.from(fixture),
  });
  await button('预览导入', modal()).click();
  await shown(modal().getByText('已读取 YAML', { exact: true }));
  await shown(importPreview().getByText(/🚀 🎯 🐱/u));
  await finish('创建配置');
  await shown(page.getByRole('heading', { name: profile }));
  await shown(page.getByRole('heading', { name: '代理组' }));
  await shown(preview());
  await shown(preview().getByText(/🚀 🎯 🐱/u));
  await shown(button('编辑🎯 表情分组'));
  await button('编辑🎯 表情分组').click();
  const emojiMember = modal().getByRole('group', { name: '显式成员' }).getByRole('checkbox', { name: '🐱 表情节点', exact: true });
  verify(await emojiMember.isChecked(), '代理组成员未保留 emoji 节点引用');
  await close();
  await section('扩展配置');
  verify((await field('扩展 YAML').inputValue()).includes('🚀 🎯 🐱'), '扩展 YAML 字段未显示 emoji');
  const theme = button('深色皮肤');
  await theme.click();
  verify(await theme.getAttribute('aria-pressed') === 'true', '深色皮肤切换失败');
  const profilesRoute = '**/api/v1/profiles?offset=0';
  await page.route(profilesRoute, async route => {
    const response = await route.fetch();
    const list = await response.json();
    await route.fulfill({ response, json: { ...list, profiles: list.profiles.map(item => ({ ...item, hasSource: true })) } });
  });
  await page.reload();
  await shown(theme);
  await page.unroute(profilesRoute);
  verify(await theme.getAttribute('aria-pressed') === 'true', '刷新后没有保留主题');
  verify(await page.locator('html').getAttribute('data-theme') === 'dark', '页面没有应用深色皮肤');
  await theme.click();
  verify(await theme.getAttribute('aria-pressed') === 'false', '浅色皮肤切换失败');
  verify((await preview().innerText()).includes('mode: Rule'), '文件导入应保留模式的原始写法');

  const refreshRoute = '**/api/v1/profiles/*/imports/refresh';
  await page.route(refreshRoute, route => route.fulfill({
    json: { id: 'test-refresh', draftRev: 1, conflicts: [], yaml: fixture.replace('\\U0001F680', '\\U0001F389') },
  }));
  await button('配置管理').click();
  await button('刷新原始订阅', modal()).click();
  await modal().getByText('profile', { exact: true }).click();
  await shown(field('当前草稿', modal()).getByText(/🚀 🎯 🐱/u));
  await shown(field('上游更新预览', modal()).getByText(/🎉 🎯 🐱/u));
  await close();
  await page.unroute(refreshRoute);

  await section('常规设置');
  await field('混合端口').fill('7897');
  await button('保存草稿').click();
  await shown(preview().getByText('mixed-port: 7897', { exact: true }));
  await section('DNS');
  await field('解析器').fill('9.9.9.9');
  await field('Fake-IP 过滤').fill('+.lan\n+.example.org');
  await field('搜索Fake-IP 过滤').fill('example');
  const filterEditor = field('Fake-IP 过滤');
  await shown(button('第 2 行 +.example.org'));
  await button('第 2 行 +.example.org').click();
  verify(await filterEditor.evaluate(el => el.value.slice(el.selectionStart, el.selectionEnd)) === '+.example.org', '搜索结果未定位到规则');
  await field('搜索Fake-IP 过滤').fill('missing');
  await shown(page.getByText('没有匹配的规则', { exact: true }));
  await button('清除搜索').click();
  await shown(page.getByText('2 条规则', { exact: true }));
  await button('保存草稿').click();
  await shown(preview().getByText(/9\.9\.9\.9/));

  await section('订阅源');
  await button('添加订阅源').click();
  await field('名称', modal()).fill('测试来源');
  await field('原始订阅地址', modal()).fill('https://provider.example.invalid/proxies.yaml?token=synthetic-only');
  await field('缓存 / 本地路径', modal()).fill('./proxy_provider/test.yaml');
  await field('其他字段（YAML）', modal()).fill('header:\n  User-Agent: [Mihomo-Test]\noverride:\n  additional-prefix: "[订阅] "');
  await save();
  await shown(button('编辑测试来源'));
  const providerYAML = await preview().innerText();
  verify(providerYAML.includes('proxy-providers:') && providerYAML.includes('provider.example.invalid/proxies.yaml'),
    '引用方式未在 YAML 中保留原始订阅地址');

  const nodeSubscription = 'https://upstream.example.invalid/nodes';
  await page.route(probeRoute, route => {
    const input = route.request().postDataJSON();
    verify(input.mode === 'nodes' && input.url === nodeSubscription && input.userAgent === 'mihomo', '节点探测请求不正确');
    return route.fulfill({
      json: { kind: 'nodes', host: 'upstream.example.invalid', yaml: String.raw`proxies:
  - {name: "\U0001F30F 订阅节点", type: socks5, server: 192.0.2.30, port: 1080}
  - {name: 测试中继, type: socks5, server: 192.0.2.31, port: 1080}
` },
    });
  });
  await button('导入订阅节点').click();
  verify(await button('导入到草稿', modal()).isDisabled(), '预览前允许写入节点');
  await page.setViewportSize({ width: 375, height: 812 });
  await field('原始订阅地址', modal()).fill(nodeSubscription);
  await field('请求 User-Agent（可选）', modal()).fill('mihomo');
  await button('预览节点', modal()).click();
  await shown(modal().getByText('🌏 订阅节点', { exact: true }));
  await shown(modal().getByText(/可导入 1 个节点.*跳过 1 个同名节点/));
  verify(await modal().evaluate(dialog => dialog.scrollWidth === dialog.clientWidth), '节点导入弹窗发生横向溢出');
  await finish('导入到草稿');
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.unroute(probeRoute);
  await shown(page.getByRole('heading', { name: '代理节点' }));
  await shown(button('编辑🌏 订阅节点'));
  const importedYAML = await preview().innerText();
  verify(importedYAML.includes('🌏 订阅节点') && !importedYAML.includes('192.0.2.31') && !importedYAML.includes(nodeSubscription),
    '导入应追加新节点，保留原有同名节点');

  await button('新建节点').click();
  await field('名称', modal()).fill('测试出口');
  await field('服务器', modal()).fill('192.0.2.27');
  await field('服务器端口', modal()).fill('1080');
  await field('拨号出口', modal()).selectOption('测试中继');
  await save();
  await shown(preview().getByText('dialer-proxy: 测试中继', { exact: true }));
  await button('编辑测试中继').click();
  await field('名称', modal()).fill('重命名中继');
  await save();
  await shown(preview().getByText('dialer-proxy: 重命名中继', { exact: true }));

  await section('代理组');
  await button('编辑节点选择').click();
  await modal().getByRole('group', { name: '显式成员' }).getByRole('checkbox', { name: '测试出口', exact: true }).check();
  await modal().getByRole('group', { name: '显式成员' }).getByRole('checkbox', { name: '🌏 订阅节点', exact: true }).check();
  await button('上移 测试出口', modal()).click();
  const providerNodesRoute = '**/api/v1/imports/provider-nodes';
  let cacheReads = 0;
  let providerRefreshes = 0;
  let cachedNodes = null;
  await page.route(providerNodesRoute, route => {
    const input = route.request().postDataJSON();
    verify(input.url === 'https://provider.example.invalid/proxies.yaml?token=synthetic-only' &&
      input.userAgent === 'Mihomo-Test', '读取节点名单时未使用该来源的地址或 User-Agent');
    if (input.refresh) {
      providerRefreshes++;
      cachedNodes = providerRefreshes === 1 ? ['🌏 远程节点'] : ['🌏 更新后的节点'];
    } else {
      cacheReads++;
    }
    return route.fulfill({
      json: { nodes: cachedNodes, ...(cachedNodes ? { fetchedAt: '2026-11-21T12:00:00Z' } : {}) },
    });
  });
  await modal().getByRole('group', { name: '订阅源' }).getByRole('checkbox', { name: '测试来源', exact: true }).check();
  const defaultNode = () => field('默认节点', modal());
  await shown(modal().getByText(/Mihomo v1\.19\.28/));
  await defaultNode().click();
  const defaultOptions = () => modal().getByRole('listbox', { name: '默认节点选项' });
  const suggestions = await defaultOptions().getByRole('option').allTextContents();
  verify(suggestions.join('|') === '重命名中继|测试出口|DIRECT|🌏 订阅节点', '默认节点选项应跟随显式成员');
  await defaultOptions().getByRole('option', { name: '测试出口', exact: true }).click();
  await defaultNode().click();
  await shown(defaultOptions().getByRole('option', { name: 'DIRECT', exact: true }));
  verify(await defaultNode().inputValue() === '测试出口', '展开候选时应保留当前值');
  await defaultOptions().getByRole('option', { name: 'DIRECT', exact: true }).click();
  verify(await defaultNode().inputValue() === 'DIRECT', '已有值时应直接换选');
  await button('展开默认节点选项', modal()).click();
  await shown(defaultOptions().getByRole('option', { name: '🌏 订阅节点', exact: true }));
  await defaultNode().press('Escape');
  await defaultOptions().waitFor({ state: 'hidden' });
  await defaultNode().fill('订阅');
  await shown(defaultOptions().getByRole('option', { name: '🌏 订阅节点', exact: true }));
  verify(await defaultOptions().getByRole('option').count() === 1, '输入时应筛选候选');
  await defaultNode().press('ArrowDown');
  await defaultNode().press('Enter');
  verify(await defaultNode().inputValue() === '🌏 订阅节点', '键盘应可选择候选');
  const providerDefault = '[订阅] 🌏 远程节点';
  await page.setViewportSize({ width: 375, height: 812 });
  await button('读取订阅节点', modal()).click();
  await field('订阅节点', modal()).selectOption(providerDefault);
  verify(await defaultNode().inputValue() === providerDefault, '订阅节点没有填入默认值或没有应用节点前缀');
  verify(await modal().evaluate(dialog => dialog.scrollWidth === dialog.clientWidth), '订阅节点选择器发生横向溢出');
  await save();
  await page.setViewportSize({ width: 1440, height: 900 });
  await shown(preview().locator('code').filter({ hasText: /default-selected:\s*["']?\[订阅\] 🌏 远程节点/u }));
  await page.reload();
  await shown(button('编辑节点选择'));
  await button('编辑节点选择').click();
  verify(await defaultNode().inputValue() === providerDefault, '重新打开时应显示已保存的订阅节点');
  await page.setViewportSize({ width: 375, height: 812 });
  await defaultNode().click();
  await shown(defaultOptions().getByRole('option', { name: 'DIRECT', exact: true }));
  await defaultOptions().getByRole('option', { name: 'DIRECT', exact: true }).click();
  verify(await defaultNode().inputValue() === 'DIRECT', '手机端已有订阅默认值时也应直接换选');
  await defaultNode().fill(providerDefault);
  await defaultNode().press('Escape');
  await page.setViewportSize({ width: 1440, height: 900 });
  await shown(field('订阅节点', modal()));
  verify(await field('订阅节点', modal()).inputValue() === providerDefault && cacheReads >= 2 && providerRefreshes === 1,
    '重新打开编辑器应直接读取缓存，不应再次请求上游');
  await button('刷新节点名单', modal()).click();
  await field('订阅节点', modal()).selectOption('[订阅] 🌏 更新后的节点');
  verify(await defaultNode().inputValue() === '[订阅] 🌏 更新后的节点' && providerRefreshes === 2, '手动刷新后应提供最新节点');
  await field('类型', modal()).selectOption('url-test');
  await defaultNode().waitFor({ state: 'hidden' });
  await field('类型', modal()).selectOption('select');
  await defaultNode().fill('');
  await save();
  await preview().locator('code').filter({ hasText: 'default-selected:' }).waitFor({ state: 'hidden' });
  await button('编辑节点选择').click();
  await defaultNode().fill('测试出口');
  await save();
  await page.unroute(providerNodesRoute);
  await section('代理节点');
  await button('编辑测试出口').click();
  await field('名称', modal()).fill('改名出口');
  await save();
  await shown(preview().locator('code').filter({ hasText: 'default-selected: 改名出口' }));

  await section('规则集');
  await button('新建规则集').click();
  await field('名称', modal()).fill('测试规则集');
  await field('来源类型', modal()).selectOption('inline');
  await field('匹配类型', modal()).selectOption('domain');
  await field('规则内容', modal()).fill('+.example.net');
  await save();
  await shown(button('编辑测试规则集'));

  await section('路由规则');
  await button('批量添加').click();
  await modal().locator('textarea').fill('RULE-SET,测试规则集,节点选择\nDOMAIN-SUFFIX,example.test,DIRECT');
  await finish('添加到草稿');
  await shown(button('编辑第 4 条规则'));
  await button('上移第 4 条规则').click();
  await button('下载 YAML').click();
  await shown(modal().getByRole('heading', { name: '配置校验' }));
  await shown(modal().getByRole('heading', { name: '需要修复' }));
  await button('移至规则末尾', modal()).click();
  await modal().waitFor({ state: 'hidden' });

  await section('扩展配置');
  await field('扩展 YAML').fill('sniffer:\n  enable: false\nprofile:\n  store-selected: true\n  emoji-fixture: "🚀 🎯 🐱"');
  await button('保存草稿').click();
  await shown(preview().getByText('store-selected: true', { exact: true }));

  const downloadReady = page.waitForEvent('download');
  await button('下载 YAML').click();
  const download = await downloadReady;
  verify(download.suggestedFilename() === profile + '.yaml', '下载的文件名缺少配置名称');
  await button('发布订阅').click();
  await button('确认发布', modal()).click();
  await shown(modal().getByRole('heading', { name: '订阅链接 · v1' }));
  await button('查看链接', modal()).click();
  const endpoint = await modal().locator('textarea').inputValue();
  const address = new URL(endpoint);
  verify(address.origin === new URL(page.url()).origin, '默认订阅地址未使用当前访问的协议、主机和端口');
  const key = address.searchParams.get('key');
  address.search = '';
  verify(address.pathname.startsWith('/s/') && key, '缺少含密钥的订阅地址');
  const unauthenticated = await page.context().browser().newContext();
  try {
    const first = await unauthenticated.request.get(endpoint);
    verify(first.status() === 200 && first.headers()['content-type'].startsWith('application/yaml'), '公开订阅应返回 YAML');
    const firstYAML = await first.text();
    verify((await unauthenticated.request.get(address.href)).status() === 404, '无密钥请求读取了订阅');
    const bearer = await unauthenticated.request.get(address.href, { headers: { Authorization: 'Bearer ' + key } });
    verify(bearer.status() === 200 && (await bearer.text()) === firstYAML, 'Bearer 调用未返回发布版本');
    const groupYAML = firstYAML.split('proxy-groups:')[1].split(/\n[^\s]/)[0];
    verify(groupYAML.includes('改名出口') && groupYAML.indexOf('改名出口') < groupYAML.indexOf('DIRECT'), '显式组成员未按表单顺序导出');
    verify(groupYAML.includes('default-selected: 改名出口'), '已发布配置缺少默认节点');
    verify(firstYAML.includes('dialer-proxy: 重命名中继') && firstYAML.includes('RULE-SET,测试规则集,节点选择'), '已发布配置缺少引用或规则');
    verify(firstYAML.includes('192.0.2.30') && firstYAML.includes('use:') &&
      firstYAML.includes('https://provider.example.invalid/proxies.yaml'), '发布订阅应同时包含导入的节点和引用的订阅源');

    await close();
    await button('已发布', page.locator('.preview')).click();
    await shown(preview().getByText(/🚀 🎯 🐱/u));
    await button('当前草稿', page.locator('.preview')).click();
    await section('常规设置');
    await field('混合端口').fill('7898');
    await button('保存草稿').click();
    await shown(preview().getByText('mixed-port: 7898', { exact: true }));
    await section('扩展配置');
    const advanced = field('扩展 YAML');
    await advanced.fill((await advanced.inputValue()).replace('🚀 🎯 🐱', '🎉 🎯 🐱'));
    await button('保存草稿').click();
    await shown(preview().getByText(/🎉 🎯 🐱/u));
    await button('比较草稿与已发布版本').click();
    await modal().getByText('profile', { exact: true }).click();
    await shown(field('已发布 v1', modal()).getByText(/🚀 🎯 🐱/u));
    await shown(field('当前草稿', modal()).getByText(/🎉 🎯 🐱/u));
    await close();
    verify((await (await unauthenticated.request.get(endpoint)).text()) === firstYAML, '保存草稿影响了已发布订阅');
    await button('发布订阅').click();
    await button('确认发布', modal()).click();
    await shown(modal().getByRole('heading', { name: '订阅链接 · v2' }));
    const second = await unauthenticated.request.get(endpoint);
    verify(second.status() === 200 && (await second.text()).includes('mixed-port: 7898'), '原链接未更新至第二版');
    await button('密钥管理', modal()).click();
    const keyInput = field('当前配置密钥', modal());
    verify(await keyInput.getAttribute('type') === 'password', '订阅密钥未默认遮盖');
    await button('显示', modal()).click();
    verify(await keyInput.inputValue() === key, '密钥管理显示的密钥与链接不符');
    await button('轮换密钥', modal()).click();
    await button('确认轮换', modal()).click();
    await shown(modal().getByRole('heading', { name: '订阅密钥' }));
    const rotated = await keyInput.inputValue();
    verify(rotated !== key && (await unauthenticated.request.get(endpoint)).status() === 404, '轮换后旧密钥仍然有效');
    await close();
    await button('管理订阅链接').click();
    const replacement = address.href + '?key=' + rotated;
    verify((await unauthenticated.request.get(replacement)).status() === 200, '轮换后相同接口未接受新密钥');
    await button('吊销', modal()).click();
    await button('确认吊销', modal()).click();
    await shown(modal().getByText('已吊销', { exact: true }));
    verify((await unauthenticated.request.get(replacement)).status() === 404, '吊销后订阅仍可访问');
    await close();
  } finally {
    await unauthenticated.close();
  }

  await button('配置管理').click();
  await button('复制配置', modal()).click();
  await modal().waitFor({ state: 'hidden' });
  const copied = page.getByRole('heading', { name: /浏览器验收配置.*副本/ });
  await shown(copied);
  await button('配置管理').click();
  await button('删除', modal()).click();
  await finish('确认删除');
  await shown(page.getByRole('heading', { name: profile }));

  await page.setViewportSize({ width: 375, height: 812 });
  await theme.click();
  verify(await page.locator('html').getAttribute('data-theme') === 'dark', '手机端未应用深色皮肤');
  await field('选择模块').selectOption('proxy-groups');
  await shown(page.getByRole('heading', { name: '代理组' }));
  await button('YAML 预览').click();
  await shown(preview());
  verify(await page.evaluate(() => document.documentElement.scrollWidth === innerWidth), '375px 窗口发生页面级横向溢出');
  await button('账户').click();
  await finish('退出登录');
  await shown(page.getByRole('heading', { name: '登录配置台' }));
  await field('用户名').fill(username);
  await field('密码').fill(password);
  await button('登录').click();
  await shown(page.getByRole('heading', { name: profile }));
  verify(browserErrors.length === 0, browserErrors.join('\n'));
  return '桌面和 375px 手机端流程通过，覆盖默认节点、订阅源引用、节点导入、emoji 预览、发布与持久化';
}
