import assert from 'node:assert/strict';
import { test } from 'node:test';
import { api, APIError, clearSession } from '../src/api.ts';

test('session stores CSRF in memory and puts it only on writes', async t => {
  const calls: { path: string; options: RequestInit }[] = [];
  t.mock.method(globalThis, 'fetch', async (path: string, options: RequestInit) => {
    calls.push({ path, options });
    if (path.endsWith('/logout')) return new Response(null, { status: 204 });
    return Response.json({ username: 'tester', id: 'acct-1', csrfToken: 'temporary-csrf' });
  });
  await api.login({ username: 'tester', password: 'an example password' });
  await api.profiles();
  await api.logout();
  assert.equal(calls[0].path, '/api/v1/auth/login');
  assert.equal(calls[1].options.cache, 'no-store');
  assert.equal(new Headers(calls[1].options.headers).get('X-CSRF-Token'), null);
  assert.equal(new Headers(calls[2].options.headers).get('X-CSRF-Token'), 'temporary-csrf');
});

test('backend issue metadata survives a rejected export', async t => {
  clearSession();
  t.mock.method(globalThis, 'fetch', async () => Response.json({
    code: 'invalid_config', message: '校验失败',
    issues: [{ severity: 'error', path: 'rules[2]', message: '策略不存在' }],
  }, { status: 422 }));
  await assert.rejects(api.download('configuration', 2), error => {
    assert.ok(error instanceof APIError);
    assert.equal(error.status, 422);
    assert.equal(error.issues[0].path, 'rules[2]');
    return true;
  });
});
