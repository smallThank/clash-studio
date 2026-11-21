async (page) => {
  const button = (name, scope = page) => scope.getByRole('button', { name, exact: true });
  const dialog = () => page.getByRole('dialog');
  const group = 'VeryLongSingleToken' + 'A'.repeat(55);
  const node = 'EndpointWithoutWhitespace' + 'B'.repeat(49);
  const source = 'SourceWithoutWhitespace' + 'C'.repeat(50);
  const profile = '跨设备布局验收' + 'LongName'.repeat(4);
  const fixture = `mode: rule
mixed-port: 7890
dns:
  enable: true
  enhanced-mode: fake-ip
  fake-ip-filter:
    - '*.lan'
    - '+.${'subdomain'.repeat(15)}.example.invalid'
proxy-providers:
  ${source}:
    type: http
    url: https://example.invalid/${'segment'.repeat(25)}
proxies:
  - {name: ${node}, type: socks5, server: 192.0.2.25, port: 1080}
proxy-groups:
  - {name: ${group}, type: select, proxies: [${node}, DIRECT], use: [${source}]}
rules:
  - DOMAIN-SUFFIX,${'long'.repeat(27)}.example.invalid,${group}
  - MATCH,DIRECT
`;
  const verify = (condition, message) => { if (!condition) throw Error(message); };
  const fit = async (label, selectors) => {
    const issues = await page.evaluate((selectors) => {
      const width = document.documentElement.clientWidth;
      const issues = [];
      if (document.documentElement.scrollWidth > width + 1) issues.push('page width overflow');
      for (const selector of selectors) {
        for (const item of document.querySelectorAll(selector)) {
          const bounds = item.getBoundingClientRect();
          if (!item.getClientRects().length || !bounds.width || !bounds.height) continue;
          if (bounds.left < -1 || bounds.right > width + 1 || item.scrollWidth > item.clientWidth + 1) {
            issues.push(selector + ' overflow');
          }
        }
      }
      return issues;
    }, selectors);
    verify(issues.length === 0, label + ': ' + issues.join(', '));
  };
  const contained = ['.topbar', '.top-actions', '.profile-banner', '.listing-row', '.listing-detail', '.editor-tools',
    '.preview', '.preview-header', '.mobile-controls', '.mobile-tabs'];
  const modalAreas = ['dialog', '.dialog-body', '.dialog-actions', '.form-grid', '.form-switch', '.check-option', '.sensitive-field'];
  page.setDefaultTimeout(10000);
  await page.setViewportSize({ width: 1440, height: 900 });
  await button('创建账号').click();
  await page.getByLabel('用户名', { exact: true }).fill('layout_' + Date.now());
  await page.getByLabel('密码', { exact: true }).fill('synthetic-layout-password-87');
  await button('注册并进入').click();
  await button('新建配置', page.getByRole('main')).click();
  await dialog().getByLabel('配置名称', { exact: true }).fill(profile);
  await button('导入 YAML', dialog()).click();
  await dialog().locator('input[type=file]').setInputFiles({
    name: 'fixture-with-a-long-name-for-layout-check.yaml', mimeType: 'application/yaml', buffer: Buffer.from(fixture),
  });
  await button('预览导入', dialog()).click();
  await dialog().getByLabel('导入 YAML 预览').waitFor();
  await button('创建配置', dialog()).click();
  await dialog().waitFor({ state: 'hidden' });
  await page.getByRole('heading', { name: profile }).waitFor();

  for (const [width, height] of [[1920, 1080], [1440, 900], [1160, 768], [1011, 768], [820, 900], [781, 768], [780, 768], [640, 360], [480, 800], [390, 844], [320, 568]]) {
    await page.setViewportSize({ width, height });
    await fit(`workspace ${width}x${height}`, contained);
  }

  const edit = button('编辑' + group);
  await edit.click();
  verify(await dialog().locator('input:visible').first().isVisible(), 'edit fields are missing');
  await fit('narrow group modal', modalAreas);
  await page.setViewportSize({ width: 375, height: 480 });
  await fit('compact group modal', modalAreas);
  const footerFits = await dialog().locator('.dialog-actions').evaluate(element =>
    element.getBoundingClientRect().bottom <= innerHeight);
  verify(footerFits, 'dialog actions are below the viewport');
  const defaultNode = dialog().getByLabel('默认节点', { exact: true });
  await defaultNode.fill(node);
  await defaultNode.click();
  await dialog().getByRole('listbox', { name: '默认节点选项' }).getByRole('option', { name: 'DIRECT', exact: true }).waitFor();
  await fit('expanded default node selector', [...modalAreas, '.editable-select', '.editable-select-options']);
  await defaultNode.press('Escape');
  await button('取消', dialog()).click();

  await page.setViewportSize({ width: 1011, height: 768 });
  await page.getByRole('navigation', { name: '配置模块' }).getByRole('button', { name: /^路由规则/ }).click();
  await fit('rules at the desktop column breakpoint', contained);
  await page.setViewportSize({ width: 375, height: 480 });
  const mobileSection = page.getByLabel('选择模块');
  await mobileSection.selectOption('rules');
  await fit('mobile rules', contained);
  await button('编辑第 1 条规则').click();
  await fit('mobile rule editor', modalAreas);
  await button('取消', dialog()).click();
  await mobileSection.selectOption('proxy-providers');
  await button('编辑' + source).click();
  await button('显示', dialog()).click();
  await fit('mobile subscription source editor', modalAreas);
  await button('取消', dialog()).click();

  await button('发布订阅').click();
  await button('确认发布', dialog()).click();
  await dialog().getByRole('heading', { name: '订阅链接 · v1' }).waitFor();
  await fit('mobile subscription links', [...modalAreas, '.link-item']);
  await button('查看链接', dialog()).click();
  await dialog().getByRole('heading', { name: '订阅地址' }).waitFor();
  await fit('mobile subscription address', modalAreas);
  await dialog().getByRole('button', { name: '关闭', exact: true }).last().click();

  await mobileSection.selectOption('dns');
  await page.getByLabel('增强模式').selectOption('redir-host');
  verify(await page.getByLabel('增强模式').inputValue() === 'redir-host', 'mobile DNS selector failed');
  await page.getByLabel('搜索Fake-IP 过滤').fill('example');
  await fit('mobile DNS search', [...contained, '.field-select', '.line-editor', '.line-editor-toolbar', '.line-editor-search', '.line-editor-match']);
  await page.getByLabel('搜索Fake-IP 过滤').press('Enter');
  verify(await page.getByLabel('Fake-IP 过滤', { exact: true }).evaluate(el =>
    el.selectionStart > 0 && el.selectionEnd > el.selectionStart), 'keyboard search did not jump to the matching rule');

  await button('深色皮肤').click();
  await button('YAML 预览').click();
  const code = page.getByRole('region', { name: '生成的 YAML' });
  verify(await code.evaluate(el => el.scrollWidth > el.clientWidth), 'long YAML lines must scroll inside the preview');
  await fit('mobile dark preview', contained);
  await page.setViewportSize({ width: 640, height: 360 });
  await fit('landscape dark preview', contained);

  await page.setViewportSize({ width: 960, height: 720 });
  await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
  await fit('200% text on a medium window', contained);
  await page.setViewportSize({ width: 320, height: 568 });
  await button('编辑配置').click();
  await fit('200% text on a narrow window', contained);
  await button('配置管理').click();
  await fit('200% text in configuration management', modalAreas);
  await button('取消', dialog()).click();
  await page.evaluate(() => { document.documentElement.style.fontSize = ''; });
  return 'Responsive layout passed at 320–1920px, landscape, 200% text and both themes.';
}
