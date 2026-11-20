export type Target = 'openclash' | 'verge' | 'both';
export type SourceMode = 'config' | 'provider';
export type RenameKind = 'proxies' | 'proxy-groups' | 'proxy-providers' | 'rule-providers';

export interface Account {
  id: string;
  username: string;
  csrfToken: string;
}

export interface Profile {
  id: string;
  name: string;
  target: Target;
  draftRev: number;
  publishedVersion?: number;
  updatedAt: string;
  hasSource: boolean;
}

export interface Issue {
  severity: 'error' | 'warning';
  path: string;
  message: string;
}

export interface Draft {
  draftRev: number;
  yaml: string;
}

export interface Preview extends Draft {
  issues: Issue[];
}

export interface Version {
  version: number;
  draftRev: number;
  createdAt: string;
}

export interface SubscriptionKey {
  key: string;
  updatedAt: string;
}

export interface SubscriptionLink {
  id: string;
  label: string;
  createdAt: string;
  revokedAt?: string;
}

export interface Rename {
  kind: RenameKind;
  from: string;
  to: string;
}

export interface Patch {
  changes: Record<string, unknown>;
  remove: string[];
  rename?: Rename;
}

export interface Probe {
  id?: string;
  kind: SourceMode;
  yaml?: string;
  host?: string;
  issues?: Issue[];
}

export interface RefreshPreview {
  id: string;
  draftRev: number;
  yaml: string;
  conflicts: { path: string; message: string }[];
}

type ProfileInput = Pick<Profile, 'name' | 'target'>;
type Credentials = Pick<Account, 'username'> & { password: string };

export class APIError extends Error {
  readonly status: number;
  readonly code: string;
  readonly issues: Issue[];

  constructor(status: number, code: string, message: string, issues: Issue[] = []) {
    super(message);
    this.status = status;
    this.code = code;
    this.issues = issues;
  }
}

let csrfToken = '';
const profilePath = (id: string, suffix = '') => '/profiles/' + encodeURIComponent(id) + suffix;
const linkPath = (id: string, link: string, suffix = '') => profilePath(id, '/links/' + encodeURIComponent(link) + suffix);

async function request(path: string, method = 'GET', body?: unknown): Promise<Response> {
  const headers = new Headers();
  if (body !== undefined) headers.set('Content-Type', 'application/json');
  if (method !== 'GET' && csrfToken) headers.set('X-CSRF-Token', csrfToken);

  let response;
  try {
    response = await fetch('/api/v1' + path, {
      method, headers, credentials: 'same-origin', cache: 'no-store',
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new APIError(0, 'NETWORK', '连接服务失败。请检查网络后重试。');
  }

  if (!response.ok) {
    const error = await response.json().catch(() => ({}));
    const message = response.status >= 500 ? '服务暂时不可用，请稍后重试。' : `请求失败（${response.status}）`;
    throw new APIError(response.status, error.code ?? 'HTTP', error.message ?? message, error.issues);
  }
  return response;
}

async function json<T>(path: string, method?: string, body?: unknown): Promise<T> {
  const response = await request(path, method, body);
  return response.json() as Promise<T>;
}

async function session(path: string, method?: string, body?: Credentials) {
  const account = await json<Account>(path, method, body);
  csrfToken = account.csrfToken;
  return account;
}

export function clearSession() {
  csrfToken = '';
}

export const api = {
  me: () => session('/auth/me'),
  login: (credentials: Credentials) => session('/auth/login', 'POST', credentials),
  register: (credentials: Credentials) => session('/auth/register', 'POST', credentials),
  async logout() {
    await request('/auth/logout', 'POST', {});
    clearSession();
  },
  profiles: (offset = 0) => json<{ profiles: Profile[]; nextOffset?: number }>('/profiles?offset=' + offset),
  profile: (id: string) => json<Profile>(profilePath(id)),
  createProfile: (profile: ProfileInput & { yaml?: string; importId?: string }) => json<Profile>('/profiles', 'POST', profile),
  updateProfile: (id: string, profile: ProfileInput) => json<Profile>(profilePath(id), 'PATCH', profile),
  deleteProfile: (id: string) => request(profilePath(id), 'DELETE'),
  duplicateProfile: (id: string) => json<Profile>(profilePath(id, '/duplicate'), 'POST', {}),
  draft: (id: string) => json<Draft>(profilePath(id, '/draft')),
  patchDraft: (id: string, expectedDraftRev: number, patch: Patch) =>
    json<Draft>(profilePath(id, '/draft'), 'PATCH', { expectedDraftRev, ...patch }),
  preview: (id: string, revision: { rev: number } | { version: number }) =>
    json<Preview>(profilePath(id, '/preview') + '?' + new URLSearchParams(Object.entries(revision).map(([key, value]) => [key, String(value)]))),
  download: async (id: string, draftRev: number) => {
    const response = await request(profilePath(id, '/export.yaml') + '?rev=' + draftRev);
    return response.blob();
  },
  publish: (id: string, expectedDraftRev: number) =>
    json<Version>(profilePath(id, '/publish'), 'POST', { expectedDraftRev }),
  versions: (id: string) => json<{ versions: Version[] }>(profilePath(id, '/versions')),
  restore: (id: string, version: number, expectedDraftRev: number) =>
    json<Draft>(profilePath(id, '/versions/' + version + '/restore'), 'POST', { expectedDraftRev }),
  links: (id: string) => json<{ links: SubscriptionLink[] }>(profilePath(id, '/links')),
  addLink: (id: string, label: string) => json<SubscriptionLink>(profilePath(id, '/links'), 'POST', { label }),
  async linkURL(id: string, link: string) {
    const result = await json<{ url: string; endpoint: string }>(linkPath(id, link, '/url?relative=1'));
    const origin = window.location.origin;
    return { url: new URL(result.url, origin).href, endpoint: new URL(result.endpoint, origin).href };
  },
  subscriptionKey: (id: string) => json<SubscriptionKey>(profilePath(id, '/subscription-key')),
  rotateSubscriptionKey: (id: string) => json<SubscriptionKey>(profilePath(id, '/subscription-key/rotate'), 'POST', {}),
  revokeLink: (id: string, link: string) => request(linkPath(id, link), 'DELETE'),
  probe: (input: { mode: SourceMode; yaml?: string; url?: string; userAgent?: string }) =>
    json<Probe>('/imports/probe', 'POST', input),
  previewRefresh: (id: string) => json<RefreshPreview>(profilePath(id, '/imports/refresh'), 'POST', { action: 'preview' }),
  applyRefresh: (id: string, importId: string, expectedDraftRev: number, resolutions: Record<string, 'draft' | 'upstream'>) =>
    json<Draft>(profilePath(id, '/imports/refresh'), 'POST', { action: 'apply', importId, expectedDraftRev, resolutions }),
};
