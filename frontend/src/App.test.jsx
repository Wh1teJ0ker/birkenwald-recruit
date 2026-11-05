import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { App as AntdApp } from 'antd';
import App from './App';

const response = (status, data) => ({
  ok: status >= 200 && status < 300,
  status,
  json: async () => data,
});

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

  it('shows the administrator campaign, task, plugin and evaluation workspace', async () => {
    vi.stubGlobal('fetch', vi.fn(async url => {
      if (url.endsWith('/me')) return response(200, { id: 1, name: '管理员', role: 'admin', csrf_token: 'csrf-admin' });
      if (url.endsWith('/campaigns')) return response(200, []);
      if (url.endsWith('/admin/plugins')) return response(200, [{ id: 1, name: 'example-check', version: '0.1.0' }]);
      if (url.endsWith('/admin/submissions')) return response(200, []);
      throw new Error('Unexpected fetch: ' + url);
    }));

    render(<AntdApp><App /></AntdApp>);
    expect(await screen.findByText('创建招新轮次')).toBeTruthy();
    expect(screen.getByText('创建并发布任务')).toBeTruthy();
    expect(screen.getByText('提交与评测')).toBeTruthy();
    expect(screen.getByText('v0.1.0 · React + Ant Design')).toBeTruthy();
  });
});
