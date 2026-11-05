import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { App as AntdApp } from 'antd';
import App from './App';

const response = (status, data) => ({
  ok: status >= 200 && status < 300,
  status,
  json: async () => data,
});

const SITE = { site_name: 'Birkenwald', season_year: '2026', registration_enabled: true };
const SETTINGS = {
  ...SITE,
  version: '0.1.0',
  go_version: 'go1.27.3',
  database_version: '18.6',
  database_time: '2026-11-05T12:00:00Z',
  email_dev_mode: true,
  smtp_configured: false,
  smtp_host: '',
  smtp_port: 587,
  smtp_username: '',
  smtp_from: 'no-reply@example.org',
  smtp_password_set: false,
  smtp_password_saved: false,
  cookie_secure: false,
  plugin_timeout: '45s',
  counts: {
    students: 1, admins: 1, disabled_users: 0, open_campaigns: 0,
    published_tasks: 0, submissions: 0, failed_evaluations: 0,
  },
};

beforeEach(() => {
  const getComputedStyle = window.getComputedStyle.bind(window);
  vi.spyOn(window, 'getComputedStyle').mockImplementation(element => getComputedStyle(element));
  vi.stubGlobal('matchMedia', vi.fn().mockImplementation(query => ({
    matches: false, media: query, onchange: null,
    addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn(),
  })));
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('Birkenwald React + Ant Design MVP', () => {
  it('logs in a student, shows tasks, and sends a CSRF-protected selection', async () => {
    let signedIn = false;
    let selected = false;
    const calls = [];
    vi.stubGlobal('fetch', vi.fn(async (url, options = {}) => {
      calls.push({ url, options });
      if (url.endsWith('/config')) return response(200, SITE);
      if (url.endsWith('/auth/login')) { signedIn = true; return response(200, { id: 1, role: 'student' }); }
      if (url.endsWith('/me')) return signedIn ? response(200, { id: 1, name: '同学', role: 'student', csrf_token: 'csrf-123' }) : response(401, { error: 'login required' });
      if (url.endsWith('/campaigns')) return response(200, [{ id: 5, name: '2026 秋招', closes_at: '2026-12-31T00:00:00Z' }]);
      if (url.endsWith('/me/selections')) return response(200, selected ? [{ id: 9, campaign_id: 5, task_id: 8, task_title: 'Go 后端', submission_id: null, score: null }] : []);
      if (url.endsWith('/campaigns/5/tasks')) return response(200, [{ id: 8, title: 'Go 后端', description: '完成一道题', deadline_at: '2026-12-30T00:00:00Z', capacity: 1, selected_count: selected ? 1 : 0 }]);
      if (url.endsWith('/campaigns/5/selections')) { selected = true; return response(201, { id: 9, task_id: 8 }); }
      throw new Error('Unexpected fetch: ' + url);
    }));

    render(<AntdApp><App /></AntdApp>);
    await screen.findByRole('button', { name: /登\s*录/ });
    fireEvent.change(screen.getAllByLabelText('邮箱')[0], { target: { value: 'student@example.org' } });
    fireEvent.change(screen.getAllByLabelText('密码')[0], { target: { value: 'long-password-123' } });
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }));
    await screen.findByRole('button', { name: '选择任务' });
    fireEvent.click(screen.getByRole('button', { name: '选择任务' }));
    await screen.findByText('我的报名与提交');
    await waitFor(() => expect(calls.some(call => call.url.endsWith('/campaigns/5/selections'))).toBe(true));
    const selection = calls.find(call => call.url.endsWith('/campaigns/5/selections'));
    expect(selection.options.headers.get('X-CSRF-Token')).toBe('csrf-123');
    expect(JSON.parse(selection.options.body)).toEqual({ task_id: 8 });
  });

  it('registers a student with an email verification code', async () => {
    let registered = false;
    const calls = [];
    vi.stubGlobal('fetch', vi.fn(async (url, options = {}) => {
      calls.push({ url, options });
      if (url.endsWith('/config')) return response(200, SITE);
      if (url.endsWith('/me')) {
        return registered
          ? response(200, { id: 1, name: '新同学', role: 'student', csrf_token: 'csrf-new' })
          : response(401, { error: 'login required' });
      }
      if (url.endsWith('/auth/verification-code')) return response(202, { ok: true, dev_code: '123456', expires_in: 600 });
      if (url.endsWith('/auth/register')) { registered = true; return response(201, { id: 1, role: 'student' }); }
      if (url.endsWith('/campaigns')) return response(200, []);
      if (url.endsWith('/me/selections')) return response(200, []);
      throw new Error('Unexpected fetch: ' + url);
    }));

    render(<AntdApp><App /></AntdApp>);
    await screen.findByRole('button', { name: /登\s*录/ });
    fireEvent.click(screen.getByRole('button', { name: '创建学生账号' }));
    fireEvent.change(await screen.findByLabelText('姓名'), { target: { value: '新同学' } });
    fireEvent.change(screen.getByLabelText('邮箱'), { target: { value: 'new@example.org' } });
    fireEvent.click(screen.getByRole('button', { name: '发送验证码' }));
    expect(await screen.findByText('123456')).toBeTruthy();
    const codeRequest = calls.find(call => call.url.endsWith('/auth/verification-code'));
    expect(JSON.parse(codeRequest.options.body)).toEqual({ email: 'new@example.org' });
    fireEvent.change(screen.getByLabelText('邮箱验证码'), { target: { value: '123456' } });
    fireEvent.change(screen.getByLabelText('密码（至少 12 位）'), { target: { value: 'long-password-123' } });
    fireEvent.click(screen.getByRole('button', { name: /注\s*册/ }));
    await waitFor(() => expect(calls.some(call => call.url.endsWith('/auth/register'))).toBe(true));
    const registerRequest = calls.find(call => call.url.endsWith('/auth/register'));
    expect(JSON.parse(registerRequest.options.body)).toMatchObject({
      email: 'new@example.org', name: '新同学', code: '123456',
    });
    expect(await screen.findByRole('heading', { name: '你好，新同学' })).toBeTruthy();
  });

  it('shows the administrator tabs, people and settings', async () => {
    vi.stubGlobal('fetch', vi.fn(async url => {
      if (url.endsWith('/config')) return response(200, SITE);
      if (url.endsWith('/me')) return response(200, { id: 1, name: '管理员', role: 'admin', csrf_token: 'csrf-admin' });
      if (url.endsWith('/campaigns')) return response(200, []);
      if (url.includes('/admin/plugins')) return response(200, [{ id: 1, name: 'example-check', version: '0.1.0', library_path: 'plugins/example/example.so', config_schema: {}, enabled: true }]);
      if (url.endsWith('/admin/submissions')) return response(200, []);
      if (url.startsWith('/api/v1/admin/users')) return response(200, [{
        id: 2, email: 'student@example.org', name: '李明', role: 'student', disabled: false,
        created_at: '2026-09-01T00:00:00Z', selection_count: 1, submission_count: 0,
      }]);
      if (url.endsWith('/admin/settings')) return response(200, SETTINGS);
      throw new Error('Unexpected fetch: ' + url);
    }));

    render(<AntdApp><App /></AntdApp>);
    expect(await screen.findByRole('heading', { name: '管理台' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '轮次与任务' }));
    expect(await screen.findByText('创建招新轮次')).toBeTruthy();
    expect(screen.getByText('创建并发布任务')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '插件' }));
    expect(await screen.findByRole('heading', { name: '插件管理' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '上传并编译' })).toBeTruthy();
    expect(screen.getByRole('link', { name: '示例插件包 .zip' }).getAttribute('href')).toBe('/api/v1/admin/plugins/example/package');
    expect(screen.getByRole('link', { name: '插件 .so' }).getAttribute('href')).toBe('/api/v1/admin/plugins/example/binary');
    expect(screen.getByText('example-check')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '人员' }));
    expect(await screen.findByText('李明')).toBeTruthy();
    expect(screen.getByText('student@example.org')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '提交与评测' }));
    expect(await screen.findByRole('heading', { name: '提交与评测' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '基础设置' }));
    expect(await screen.findByText('站点设置')).toBeTruthy();
    expect(screen.getByText('邮件与注册')).toBeTruthy();
    expect(screen.getByLabelText('SMTP 主机')).toBeTruthy();
    expect(screen.getByText('example-check v0.1.0')).toBeTruthy();
  });

  it('uses configured site copy and hides registration when closed', async () => {
    vi.stubGlobal('fetch', vi.fn(async url => {
      if (url.endsWith('/config')) return response(200, { site_name: '白桦林', season_year: '2027', registration_enabled: false });
      if (url.endsWith('/me')) return response(401, { error: 'login required' });
      throw new Error('Unexpected fetch: ' + url);
    }));

    render(<AntdApp><App /></AntdApp>);
    const banner = await screen.findByRole('banner');
    expect(await within(banner).findByText('白桦林')).toBeTruthy();
    expect(within(banner).getByText('2027 招新季')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '创建学生账号' })).toBeNull();
    expect(screen.getByText('注册暂未开放')).toBeTruthy();
  });

  it('edits a task through the admin task modal', async () => {
    const calls = [];
    const campaign = {
      id: 5, name: '2026 秋招', status: 'open',
      opens_at: '2026-09-01T00:00:00Z', closes_at: '2026-12-31T00:00:00Z',
    };
    const task = {
      id: 8, title: 'Go 后端', description: '完成题目', status: 'published',
      capacity: 2, selected_count: 1, deadline_at: '2026-12-30T00:00:00Z', plugin_version_id: 1,
    };
    vi.stubGlobal('fetch', vi.fn(async (url, options = {}) => {
      calls.push({ url, options });
      if (url.endsWith('/config')) return response(200, SITE);
      if (url.endsWith('/me')) return response(200, { id: 1, name: '管理员', role: 'admin', csrf_token: 'csrf-admin' });
      if (url.endsWith('/campaigns')) return response(200, [campaign]);
      if (url.endsWith('/campaigns/5/tasks')) return response(200, [task]);
      if (url.includes('/admin/plugins')) return response(200, [{ id: 1, name: 'example-check', version: '0.1.0', library_path: 'plugins/example/example.so', config_schema: {}, enabled: true }]);
      if (url.endsWith('/admin/submissions')) return response(200, []);
      if (url.endsWith('/admin/tasks/8') && options.method === 'PATCH') return response(200, { id: 8, status: 'published' });
      throw new Error('Unexpected fetch: ' + url);
    }));

    render(<AntdApp><App /></AntdApp>);
    fireEvent.click(await screen.findByRole('button', { name: '轮次与任务' }));
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('标题'), { target: { value: 'Go 后端进阶' } });
    fireEvent.click(within(dialog).getByRole('button', { name: /保\s*存/ }));
    await waitFor(() => expect(calls.some(call => call.url.endsWith('/admin/tasks/8'))).toBe(true));
    const patch = calls.find(call => call.url.endsWith('/admin/tasks/8'));
    expect(JSON.parse(patch.options.body)).toMatchObject({
      title: 'Go 后端进阶', capacity: 2, status: 'published',
    });
  });
});
