import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import {
  App as AntdApp, Button, Checkbox, DatePicker, Form, Input, InputNumber, Modal,
  Select, Spin, Switch, Table, Upload,
} from 'antd';
import dayjs from 'dayjs';
import packageInfo from '../package.json';
import BirchScene from './BirchScene.jsx';

const STATUS_TEXT = {
  draft: '筹备中',
  open: '开放中',
  closed: '已结束',
  archived: '已归档',
  published: '已发布',
  submitted: '已提交',
  evaluating: '评测中',
  evaluated: '已完成',
  queued: '排队中',
  running: '评测中',
  succeeded: '已完成',
  failed: '评测失败',
};

const ROLE_TEXT = { admin: '管理员', student: '学生' };
const DEFAULT_SITE = { site_name: 'Birkenwald', season_year: '2026', registration_enabled: true, version: packageInfo.version };
const ADMIN_TABS = [
  { id: 'overview', label: '概览' },
  { id: 'tasks', label: '轮次与任务' },
  { id: 'plugins', label: '插件' },
  { id: 'people', label: '人员' },
  { id: 'submissions', label: '提交与评测' },
  { id: 'settings', label: '基础设置' },
];

const AUTH_ERROR_TEXT = {
  'invalid credentials': '邮箱或密码错误',
  'account disabled': '账号已被禁用，请联系管理员',
  'registration is closed': '注册暂未开放',
  'email already registered': '该邮箱已注册',
  'valid email required': '请输入有效邮箱',
  'valid email, name and password of at least 12 characters required': '请填写有效的邮箱、姓名和至少 12 位密码',
  '6-digit verification code required': '请输入 6 位验证码',
  'verification code required': '请先获取验证码',
  'verification code expired': '验证码已过期，请重新获取',
  'verification code invalid': '验证码不正确',
  'verification code requested too recently': '发送太频繁，请稍后再试',
  'failed to send verification code': '验证码发送失败，请稍后再试',
  'email service unavailable': '邮件服务暂不可用',
};

function friendlyAuthError(text) {
  return AUTH_ERROR_TEXT[text] || text;
}

function statusLabel(value) {
  if (!value) return '未知';
  return STATUS_TEXT[value] || value;
}

function statusTone(value) {
  if (['open', 'published', 'succeeded', 'evaluated'].includes(value)) return 'success';
  if (['failed', 'closed', 'archived'].includes(value)) return 'danger';
  if (['queued', 'running', 'evaluating', 'submitted'].includes(value)) return 'info';
  return 'neutral';
}

function submissionBadge(item) {
  if (!item.submission_id) return { label: '待提交', tone: 'neutral' };
  if (item.score !== null && item.score !== undefined) return { label: '评测完成', tone: 'success' };
  const status = item.evaluation_status || item.submission_status || 'submitted';
  return { label: statusLabel(status), tone: statusTone(status) };
}

function initial(name) {
  return (name || '?').trim().slice(0, 1).toUpperCase();
}

function formatDateTime(value) {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  const pad = number => String(number).padStart(2, '0');
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function formatDate(value) {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  const pad = number => String(number).padStart(2, '0');
  return `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())}`;
}

function daysLeft(value) {
  if (!value) return null;
  const diff = new Date(value).getTime() - Date.now();
  if (Number.isNaN(diff)) return null;
  return Math.ceil(diff / 86400000);
}

function formatScore(score) {
  return String(Number(score));
}

function revealDelay(index) {
  return { '--milk-reveal-delay': `${Math.min(index * 80, 240)}ms` };
}

function useReveal(ref) {
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root || typeof IntersectionObserver === 'undefined') return;
    if (window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const nodes = Array.from(root.querySelectorAll('[data-reveal]'));
    const reveal = node => {
      if (node.dataset.revealState === 'entering') return;
      node.dataset.revealState = 'entering';
      observer.unobserve(node);
    };
    const observer = new IntersectionObserver(entries => {
      entries.forEach(entry => { if (entry.isIntersecting) reveal(entry.target); });
    }, { rootMargin: '0px 0px -8% 0px', threshold: 0.02 });
    const prepare = () => {
      nodes.forEach(node => {
        if (node.dataset.revealState === 'entering') return;
        const rect = node.getBoundingClientRect();
        if (rect.top < window.innerHeight && rect.bottom > 0) reveal(node);
        else if (node.dataset.revealState !== 'pending') {
          node.dataset.revealState = 'pending';
          observer.observe(node);
        }
      });
    };
    prepare();
    const frame = window.requestAnimationFrame(prepare);
    const finish = event => {
      if (event.animationName === 'milk-reveal-in' && event.target instanceof HTMLElement) {
        event.target.dataset.revealState = 'done';
      }
    };
    root.addEventListener('animationend', finish);
    // Never leave content hidden if an observer callback is missed (embedded browsers).
    const fallback = window.setTimeout(() => nodes.forEach(reveal), 900);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(fallback);
      root.removeEventListener('animationend', finish);
      observer.disconnect();
    };
  }, [ref]);
}

async function call(path, options = {}, csrf = '') {
  const headers = new Headers(options.headers || {});
  if (options.body && !(options.body instanceof FormData)) headers.set('Content-Type', 'application/json');
  if (csrf && options.method && !['GET', 'HEAD'].includes(options.method)) headers.set('X-CSRF-Token', csrf);
  const response = await fetch('/api/v1' + path, { credentials: 'same-origin', ...options, headers });
  const data = await response.json().catch(() => ({ error: '服务器响应无效' }));
  if (!response.ok) {
    const error = new Error(data.error || '请求失败');
    error.status = response.status;
    throw error;
  }
  return data;
}

function LineIcon({ children, size = 16 }) {
  return (
    <svg className="icon" width={size} height={size} viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {children}
    </svg>
  );
}

const CalendarIcon = () => (
  <LineIcon>
    <rect x="3" y="5" width="18" height="16" rx="2" />
    <path d="M8 3v4M16 3v4M3 10h18" />
  </LineIcon>
);

const UsersIcon = () => (
  <LineIcon>
    <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
    <circle cx="9" cy="7" r="4" />
    <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
  </LineIcon>
);

const FileIcon = () => (
  <LineIcon>
    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
    <path d="M14 2v6h6M9 13h6M9 17h4" />
  </LineIcon>
);

const ArchiveIcon = () => (
  <LineIcon>
    <path d="M21 8v13H3V8M1 3h22v5H1z" />
    <path d="M10 12h4" />
  </LineIcon>
);

const AlertIcon = () => (
  <LineIcon>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 8v5M12 16.5v.01" />
  </LineIcon>
);

const UploadIcon = () => (
  <LineIcon>
    <path d="M12 16V4M7 9l5-5 5 5" />
    <path d="M4 16v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3" />
  </LineIcon>
);

function BrandMark() {
  return (
    <span className="brand-mark" aria-hidden="true">
      <svg viewBox="0 0 32 32" width="26" height="26" fill="none">
        <g stroke="currentColor" strokeWidth="1.3" strokeLinecap="round">
          <path d="M10 24V12.5M16 24V8.5M22 24V15" />
          <path d="M10 16.6l2.2-2.4M10 20.2l-2-2.2M16 13.4l2.4-2.6M16 18.2l-2.2-2.4M22 19.6l2-2.2M22 22.4l-1.8-2" />
        </g>
      </svg>
    </span>
  );
}

function GlowInput(props) {
  return <span className="milk-field"><Input {...props} /></span>;
}

function GlowPassword(props) {
  return <span className="milk-field"><Input.Password {...props} /></span>;
}

function GlowTextArea(props) {
  return <span className="milk-field"><Input.TextArea {...props} /></span>;
}

function GlowDatePicker(props) {
  return <span className="milk-field"><DatePicker {...props} /></span>;
}

function GlowNumber(props) {
  return <span className="milk-field"><InputNumber {...props} /></span>;
}

function CodeInput({ value, onChange, sendCode, sending, countdown, ...rest }) {
  return <div className="code-row">
    <span className="milk-field">
      <Input {...rest} value={value ?? ''} onChange={onChange} size="large" maxLength={6}
        inputMode="numeric" autoComplete="one-time-code" placeholder="6 位数字" />
    </span>
    <Button className="code-button" htmlType="button" onClick={sendCode}
      loading={sending} disabled={sending || countdown > 0}>
      {countdown > 0 ? `${countdown}s 后重发` : '发送验证码'}
    </Button>
  </div>;
}

function Pill({ tone = 'neutral', dot = false, children }) {
  return (
    <span className={`pill pill-${tone}`}>
      {dot && <span className="pill-dot" />}
      {children}
    </span>
  );
}

function Stat({ label, value, tone }) {
  return (
    <div className={`stat${tone ? ' is-' + tone : ''}`}>
      <span className="stat-value">{value}</span>
      <span className="stat-label">{label}</span>
    </div>
  );
}

function SectionHead({ eyebrow, title, hint }) {
  return (
    <header className="section-head" data-reveal>
      <div>
        <span className="eyebrow">{eyebrow}</span>
        <h2>{title}</h2>
      </div>
      {hint && <span className="section-hint">{hint}</span>}
    </header>
  );
}

function EmptyState({ title, hint }) {
  return (
    <div className="empty-state" data-reveal>
      <span className="empty-art" aria-hidden="true">
        <svg viewBox="0 0 96 72" width="96" height="72" fill="none" stroke="currentColor"
          strokeWidth="1.1" strokeLinecap="round">
          <path d="M22 58V26l10-8 10 8v32M32 58V34" />
          <path d="M50 58V18l8-6 8 6v40M58 58V28" />
          <path d="M70 58V38l8-5 8 5v20" />
          <path d="M10 58h78" />
          <path d="M14 64h68" opacity=".5" />
        </svg>
      </span>
      <strong>{title}</strong>
      {hint && <span className="empty-hint">{hint}</span>}
    </div>
  );
}

function RegisterForm({ submit, busy, onBack }) {
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm();
  const [sending, setSending] = useState(false);
  const [countdown, setCountdown] = useState(0);
  const [devCode, setDevCode] = useState('');
  useEffect(() => {
    if (countdown <= 0) return undefined;
    const timer = window.setTimeout(() => setCountdown(value => value - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [countdown]);
  const sendCode = async () => {
    let email;
    try {
      ({ email } = await form.validateFields(['email']));
    } catch {
      message.warning('请先填写有效邮箱');
      return;
    }
    setSending(true);
    try {
      const data = await call('/auth/verification-code', { method: 'POST', body: JSON.stringify({ email }) });
      setDevCode(data.dev_code || '');
      setCountdown(60);
      message.success('验证码已发送');
    } catch (error) { message.error(friendlyAuthError(error.message)); }
    finally { setSending(false); }
  };
  return <>
    <span className="eyebrow">New student</span>
    <h2 className="auth-title">创建账号</h2>
    <p className="auth-sub">注册后查看开放任务。</p>
    <Form form={form} name="register" layout="vertical" requiredMark={false} onFinish={values => submit('/auth/register', values, '注册成功')}>
      <Form.Item label="姓名" name="name" rules={[{ required: true }]}>
        <GlowInput size="large" maxLength={100} placeholder="你的姓名" />
      </Form.Item>
      <Form.Item label="邮箱" name="email" rules={[{ required: true, type: 'email' }]}>
        <GlowInput size="large" autoComplete="email" placeholder="you@example.com" />
      </Form.Item>
      <Form.Item label="邮箱验证码" name="code" rules={[
        { required: true, message: '请输入验证码' },
        { pattern: /^\d{6}$/, message: '验证码为 6 位数字' },
      ]}>
        <CodeInput sendCode={sendCode} sending={sending} countdown={countdown} />
      </Form.Item>
      {devCode && <div className="dev-code">开发模式验证码：<strong>{devCode}</strong></div>}
      <Form.Item label="密码（至少 12 位）" name="password" rules={[{ required: true, min: 12 }]}>
        <GlowPassword size="large" autoComplete="new-password" placeholder="至少 12 位字符" />
      </Form.Item>
      <Button className="auth-submit" type="primary" size="large" block htmlType="submit" loading={busy}>注册</Button>
    </Form>
    <div className="auth-switch">
      已有账号？
      <button type="button" className="link-button" onClick={onBack}>返回登录</button>
    </div>
  </>;
}

function Auth({ onSignedIn, site = DEFAULT_SITE }) {
  const { message } = AntdApp.useApp();
  const rootRef = useRef(null);
  const [mode, setMode] = useState('login');
  const [busy, setBusy] = useState(false);
  useReveal(rootRef);
  const submit = async (path, values, successText) => {
    setBusy(true);
    try {
      await call(path, { method: 'POST', body: JSON.stringify(values) });
      message.success(successText);
      await onSignedIn();
    } catch (error) { message.error(friendlyAuthError(error.message)); }
    finally { setBusy(false); }
  };
  return <div className="auth-shell" ref={rootRef}>
    <section className="auth-hero milk-panel" data-reveal>
      <BirchScene />
      <div className="auth-copy">
        <span className="eyebrow">{site.site_name} Recruit · {site.season_year}</span>
        <h1>认真对待<br />每一次选择。</h1>
        <p className="lede">从选题到评测，流程清晰可追溯。</p>
      </div>
    </section>
    <section className="auth-panel milk-panel" data-reveal style={revealDelay(1)}>
      {mode === 'login' ? <>
        <span className="eyebrow">Welcome back</span>
        <h2 className="auth-title">登录</h2>
        <p className="auth-sub">使用邮箱继续。</p>
        <Form name="login" layout="vertical" requiredMark={false} onFinish={values => submit('/auth/login', values, '登录成功')}>
          <Form.Item label="邮箱" name="email" rules={[{ required: true, type: 'email' }]}>
            <GlowInput size="large" autoComplete="email" placeholder="you@example.com" />
          </Form.Item>
          <Form.Item label="密码" name="password" rules={[{ required: true }]}>
            <GlowPassword size="large" autoComplete="current-password" placeholder="输入密码" />
          </Form.Item>
          <Button className="auth-submit" type="primary" size="large" block htmlType="submit" loading={busy}>登录</Button>
        </Form>
        {site.registration_enabled
          ? <div className="auth-switch">
            还没有账号？
            <button type="button" className="link-button" onClick={() => setMode('register')}>创建学生账号</button>
          </div>
          : <div className="auth-switch auth-switch-muted">注册暂未开放</div>}
      </> : <RegisterForm submit={submit} busy={busy} onBack={() => setMode('login')} />}
      <div className="auth-note">会话 7 天 · 文件私有存储</div>
    </section>
  </div>;
}

function Dropzone({ file, icon, title, hint, action }) {
  return <span className={`dropzone${file ? ' has-file' : ''}`}>
    <span className="dropzone-icon">{icon}</span>
    <span className="dropzone-body">
      <strong>{file ? file.name : title}</strong>
      <small>{file ? `${(file.size / 1024 / 1024).toFixed(2)} MB · 已选择` : hint}</small>
    </span>
    <span className="dropzone-action">
      {file ? <>{action}<UploadIcon /></> : action}
    </span>
  </span>;
}

function SelectionCard({ item, csrf, refresh, index = 0 }) {
  const { message } = AntdApp.useApp();
  const [resume, setResume] = useState([]);
  const [pack, setPack] = useState([]);
  const [busy, setBusy] = useState(false);
  const uploadProps = (files, setFiles, accept, limitMB) => ({
    accept, maxCount: 1, fileList: files, showUploadList: false,
    beforeUpload: file => {
      if (file.size > limitMB * 1024 * 1024) {
        message.error(`文件不能超过 ${limitMB} MB`);
        return Upload.LIST_IGNORE;
      }
      return false;
    },
    onChange: ({ fileList }) => setFiles(fileList.slice(-1)),
  });
  const uploadFile = async (file, kind) => {
    const body = new FormData();
    body.append('kind', kind);
    body.append('file', file.originFileObj || file);
    return call('/files', { method: 'POST', body }, csrf);
  };
  const submit = async () => {
    if (!resume.length || !pack.length) { message.warning('请选择简历与作品'); return; }
    setBusy(true);
    try {
      const [r, p] = await Promise.all([uploadFile(resume[0], 'resume'), uploadFile(pack[0], 'package')]);
      await call('/selections/' + item.id + '/submissions', {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
        body: JSON.stringify({ resume_file_id: r.id, package_file_id: p.id }),
      }, csrf);
      message.success('已提交');
      setResume([]); setPack([]);
      await refresh();
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  const badge = submissionBadge(item);
  const submitted = Boolean(item.submission_id);
  const hasScore = item.score !== null && item.score !== undefined;
  return <article className="submission-card milk-panel" data-reveal style={revealDelay(index)}>
    <header className="submission-head">
      <div>
        <span className="eyebrow">Selection · {String(item.id).padStart(2, '0')}</span>
        <h3>{item.task_title}</h3>
      </div>
      <Pill tone={badge.tone} dot>{badge.label}</Pill>
    </header>
    <div className="submission-summary">
      <div className={`score-well${hasScore ? '' : ' is-empty'}`}>
        <span className="score-value">{hasScore ? formatScore(item.score) : '—'}</span>
        <span className="score-caption">{hasScore ? '满分 100' : '等待评测'}</span>
      </div>
      <dl className="meta-list">
        <div><dt>当前版本</dt><dd>{submitted ? `第 ${item.version} 版` : '尚未提交'}</dd></div>
        <div><dt>提交时间</dt><dd>{formatDateTime(item.submitted_at)}</dd></div>
        <div><dt>评测状态</dt><dd>{badge.label}</dd></div>
      </dl>
    </div>
    {item.error && <div className="error-note"><AlertIcon /><span>{item.error}</span></div>}
    <div className="upload-grid">
      <Upload {...uploadProps(resume, setResume, '.pdf,application/pdf', 10)} className="upload-block">
        <Dropzone file={resume[0]} icon={<FileIcon />} title="简历 PDF" hint="PDF · 最大 10 MB" action={resume.length ? '重新选择' : '选择文件'} />
      </Upload>
      <Upload {...uploadProps(pack, setPack, '.zip,application/zip', 50)} className="upload-block">
        <Dropzone file={pack[0]} icon={<ArchiveIcon />} title="作品 ZIP" hint="ZIP · 最大 50 MB" action={pack.length ? '重新选择' : '选择文件'} />
      </Upload>
    </div>
    <footer className="submission-foot">
      <span className="foot-hint">截止前可反复提交，自动保留版本记录。</span>
      <Button type="primary" loading={busy} onClick={submit}>{submitted ? '提交新版本' : '上传并提交'}</Button>
    </footer>
  </article>;
}

function TaskCard({ task, index, chosen, selectedID, selecting, onSelect }) {
  const isChosen = selectedID === task.id;
  const full = task.capacity !== null && task.capacity !== undefined && task.selected_count >= task.capacity;
  const left = daysLeft(task.deadline_at);
  const deadlineTime = task.deadline_at ? new Date(task.deadline_at).getTime() : NaN;
  const closed = !Number.isNaN(deadlineTime) && deadlineTime <= Date.now();
  const disabled = chosen || full || closed;
  const label = isChosen ? '本轮已选' : chosen ? '本轮已选其他任务' : full ? '名额已满' : closed ? '已截止' : '选择任务';
  const state = isChosen
    ? { label: '已选择', tone: 'ink' }
    : closed
      ? { label: '已截止', tone: 'neutral' }
      : full
        ? { label: '名额已满', tone: 'danger' }
        : task.status && task.status !== 'published'
          ? { label: statusLabel(task.status), tone: statusTone(task.status) }
          : { label: '开放申请', tone: 'success' };
  const capacityText = task.capacity === null || task.capacity === undefined
    ? '名额不限'
    : `${task.selected_count} / ${task.capacity} 已选`;
  const ratio = task.capacity ? Math.min(100, Math.round((task.selected_count / task.capacity) * 100)) : 0;
  return <article className={`task-card milk-panel${isChosen ? ' is-chosen' : ''}`} data-reveal style={revealDelay(index)}>
    <div className="task-top">
      <span className="task-index">{String(index + 1).padStart(2, '0')}</span>
      <Pill tone={state.tone}>{state.label}</Pill>
    </div>
    <h4>{task.title}</h4>
    <p className="task-desc">{task.description || '暂无说明。'}</p>
    <div className="task-meta">
      <div className="task-meta-row"><CalendarIcon /><span>{formatDateTime(task.deadline_at)} 截止</span></div>
      <div className="task-meta-row"><UsersIcon /><span>{capacityText}</span>
        {left !== null && <span className={`task-left${left <= 3 && left >= 0 ? ' is-urgent' : ''}`}>
          {closed ? '已截止' : left === 0 ? '今天截止' : `剩余 ${left} 天`}
        </span>}
      </div>
      {task.capacity !== null && task.capacity !== undefined && (
        <span className="capacity-track"><span className="capacity-fill" style={{ width: ratio + '%' }} /></span>
      )}
    </div>
    <Button type="primary" disabled={disabled} loading={selecting} onClick={onSelect}>{label}</Button>
  </article>;
}

function Student({ name, campaigns, tasks, selections, csrf, refresh }) {
  const { message } = AntdApp.useApp();
  const rootRef = useRef(null);
  const [selecting, setSelecting] = useState(null);
  useReveal(rootRef);
  const chosen = new Map(selections.map(item => [item.campaign_id, item.task_id]));
  const openCampaigns = campaigns.filter(campaign => !campaign.status || campaign.status === 'open').length;
  const pending = selections.filter(item => item.submission_id && ['queued', 'running'].includes(item.evaluation_status)).length;
  const scored = selections.filter(item => item.score !== null && item.score !== undefined).length;
  const selectTask = async (campaignID, taskID) => {
    setSelecting(taskID);
    try {
      await call('/campaigns/' + campaignID + '/selections', { method: 'POST', body: JSON.stringify({ task_id: taskID }) }, csrf);
      message.success('已选择任务'); await refresh();
    } catch (error) { message.error(error.message); }
    finally { setSelecting(null); }
  };
  return <div ref={rootRef}>
    <section className="welcome milk-panel" data-reveal>
      <div className="welcome-copy">
        <span className="eyebrow">Student workspace</span>
        <h1>你好，{name}</h1>
        <p className="welcome-sub">选择一个任务，提交简历与作品。截止前可更新版本。</p>
      </div>
      <div className="welcome-stats">
        <Stat label="开放轮次" value={openCampaigns} />
        <Stat label="已选任务" value={selections.length} />
        <Stat label="评测中" value={pending} />
        <Stat label="已出分" value={scored} />
      </div>
    </section>
    <section className="section">
      <SectionHead eyebrow="Open tasks" title="开放任务" hint={`${openCampaigns} 个轮次开放中`} />
      {!campaigns.length && <EmptyState title="暂无开放轮次" hint="招新开始后会显示在这里。" />}
      {campaigns.map((campaign, campaignIndex) => {
        const list = tasks[campaign.id] || [];
        return <div className="campaign" key={campaign.id}>
          <header className="campaign-head" data-reveal style={revealDelay(Math.min(campaignIndex, 2))}>
            <div>
              <div className="campaign-title">
                <h3>{campaign.name}</h3>
                <Pill tone={statusTone(campaign.status)} dot>{statusLabel(campaign.status)}</Pill>
              </div>
              <p className="campaign-sub">
                <span>开放至 {formatDateTime(campaign.closes_at)}</span>
                <span className="sep" />
                <span>{list.length} 个任务</span>
              </p>
            </div>
            {chosen.has(campaign.id) && <Pill tone="ink">选题已锁定</Pill>}
          </header>
          {list.length ? <div className="task-grid">
            {list.map((task, index) => <TaskCard key={task.id} task={task} index={index}
              chosen={chosen.has(campaign.id)} selectedID={chosen.get(campaign.id)}
              selecting={selecting === task.id} onSelect={() => selectTask(campaign.id, task.id)} />)}
          </div> : <EmptyState title="暂无已发布任务" hint="发布后会显示在这里。" />}
        </div>;
      })}
    </section>
    <section className="section">
      <SectionHead eyebrow="My selections" title="我的报名与提交" hint={selections.length ? `${selections.length} 个任务` : undefined} />
      {!selections.length
        ? <EmptyState title="尚未选择任务" hint="选择上方任务后即可上传材料。" />
        : selections.map((item, index) => <SelectionCard key={item.id} item={item} csrf={csrf} refresh={refresh} index={index} />)}
    </section>
  </div>;
}

function AdminOverview({ campaigns, tasks, submissions, onOpenTab }) {
  const taskCount = Object.values(tasks).reduce((total, list) => total + list.length, 0);
  const failed = submissions.filter(row => row.evaluation_status === 'failed').length;
  return <>
    <section className="welcome milk-panel" data-reveal>
      <div className="welcome-copy">
        <span className="eyebrow">Admin workspace</span>
        <h1>管理台</h1>
        <p className="welcome-sub">管理轮次、人员与站点设置。</p>
      </div>
      <div className="welcome-stats">
        <Stat label="招新轮次" value={campaigns.length} />
        <Stat label="任务" value={taskCount} />
        <Stat label="提交" value={submissions.length} />
        <Stat label="失败评测" value={failed} tone={failed ? 'alert' : undefined} />
      </div>
    </section>
    <div className="quick-grid">
      {[
        { id: 'tasks', eyebrow: 'Campaigns', title: '轮次与任务', meta: `${campaigns.length} 个轮次 · ${taskCount} 个任务` },
        { id: 'people', eyebrow: 'People', title: '人员管理', meta: '账号、角色与状态' },
        { id: 'settings', eyebrow: 'Settings', title: '基础设置', meta: '站点、注册与运行信息' },
      ].map((item, index) => <button type="button" className="quick-card milk-panel" key={item.id}
        onClick={() => onOpenTab(item.id)} data-reveal style={revealDelay(Math.min(index, 2))}>
        <span className="eyebrow">{item.eyebrow}</span>
        <strong>{item.title}</strong>
        <small>{item.meta}</small>
        <span className="quick-arrow" aria-hidden="true">→</span>
      </button>)}
    </div>
    <section className="section">
      <SectionHead eyebrow="Campaigns" title="当前轮次" hint={`${campaigns.length} 个`} />
      {campaigns.length
        ? <div className="overview-grid">
          {campaigns.slice(0, 3).map((campaign, index) => {
            const list = tasks[campaign.id] || [];
            return <article className="overview-card milk-panel" key={campaign.id} data-reveal style={revealDelay(Math.min(index, 2))}>
              <div className="overview-card-head">
                <h3>{campaign.name}</h3>
                <Pill tone={statusTone(campaign.status)} dot>{statusLabel(campaign.status)}</Pill>
              </div>
              <p>{formatDate(campaign.opens_at)} — {formatDate(campaign.closes_at)}</p>
              <span className="overview-card-meta">{list.length} 个任务 · {list.reduce((total, task) => total + task.selected_count, 0)} 人已选</span>
            </article>;
          })}
        </div>
        : <EmptyState title="还没有招新轮次" hint="到「轮次与任务」创建。" />}
    </section>
  </>;
}

function TaskEditModal({ task, csrf, onClose, onSaved }) {
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const save = async values => {
    setBusy(true);
    try {
      await call('/admin/tasks/' + task.id, { method: 'PATCH', body: JSON.stringify({
        title: values.title.trim(),
        description: values.description || '',
        deadline_at: values.deadline_at.toISOString(),
        capacity: values.capacity || null,
        status: values.status,
      }) }, csrf);
      message.success('任务已更新');
      await onSaved();
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  return <Modal open title="编辑任务" okText="保存" cancelText="取消" width={560}
    confirmLoading={busy} onOk={() => form.submit()} onCancel={onClose}>
    <Form form={form} name="taskEdit" layout="vertical" requiredMark={false} onFinish={save} initialValues={{
      title: task.title,
      description: task.description,
      deadline_at: dayjs(task.deadline_at),
      capacity: task.capacity ?? undefined,
      status: task.status,
    }}>
      <Form.Item name="title" label="标题" rules={[{ required: true, whitespace: true, message: '请输入标题' }, { max: 200, message: '最多 200 个字符' }]}>
        <GlowInput maxLength={200} />
      </Form.Item>
      <Form.Item name="description" label="任务说明">
        <GlowTextArea rows={4} placeholder="任务目标与交付要求" />
      </Form.Item>
      <Form.Item name="deadline_at" label="提交截止时间" rules={[{ required: true, message: '请选择截止时间' }]}>
        <GlowDatePicker showTime style={{ width: '100%' }} />
      </Form.Item>
      <Form.Item name="capacity" label="名额上限（留空不限）">
        <GlowNumber min={1} style={{ width: '100%' }} placeholder="不限" />
      </Form.Item>
      <Form.Item name="status" label="任务状态" rules={[{ required: true }]}>
        <Select options={[
          { value: 'draft', label: '草稿' },
          { value: 'published', label: '已发布' },
          { value: 'closed', label: '已关闭' },
        ]} />
      </Form.Item>
    </Form>
  </Modal>;
}

function AdminTasks({ campaigns, tasks, plugins, csrf, refresh }) {
  const { message, modal } = AntdApp.useApp();
  const [campaignForm] = Form.useForm();
  const [taskForm] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [editingTask, setEditingTask] = useState(null);
  const createCampaign = async values => {
    setBusy(true);
    try {
      await call('/admin/campaigns', { method: 'POST', body: JSON.stringify({
        name: values.name, opens_at: values.opens_at.toISOString(), closes_at: values.closes_at.toISOString(), open: values.open,
      }) }, csrf);
      campaignForm.resetFields(); message.success('轮次已创建'); await refresh({ silent: true });
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  const createTask = async values => {
    setBusy(true);
    try {
      const config = JSON.parse(values.plugin_config || '{}');
      await call('/admin/tasks', { method: 'POST', body: JSON.stringify({
        campaign_id: values.campaign_id, title: values.title, description: values.description || '',
        deadline_at: values.deadline_at.toISOString(), capacity: values.capacity || null,
        plugin_version_id: values.plugin_version_id, plugin_config: config, publish: values.publish,
      }) }, csrf);
      taskForm.resetFields(); message.success('任务已创建'); await refresh({ silent: true });
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  const patchTask = (task, changes = {}) => call('/admin/tasks/' + task.id, { method: 'PATCH', body: JSON.stringify({
    title: changes.title ?? task.title,
    description: changes.description ?? task.description,
    deadline_at: changes.deadline_at ?? task.deadline_at,
    capacity: Object.prototype.hasOwnProperty.call(changes, 'capacity') ? changes.capacity : task.capacity,
    status: changes.status ?? task.status,
  }) }, csrf);
  const toggleTask = task => {
    const closing = task.status === 'published';
    const next = closing ? 'closed' : 'published';
    modal.confirm({
      title: closing ? '关闭任务？' : '发布任务？',
      content: closing ? '关闭后学生不能再提交该任务。' : '发布后任务会出现在学生端。',
      okText: closing ? '关闭任务' : '发布任务',
      cancelText: '取消',
      okButtonProps: closing ? { danger: true } : undefined,
      onOk: async () => {
        try {
          await patchTask(task, { status: next });
          message.success(closing ? '任务已关闭' : '任务已发布');
          await refresh({ silent: true });
        } catch (error) { message.error(error.message); throw error; }
      },
    });
  };
  const toggleCampaign = campaign => {
    const closing = campaign.status === 'open';
    const next = closing ? 'closed' : 'open';
    modal.confirm({
      title: closing ? '关闭轮次？' : '开放轮次？',
      content: closing ? '关闭后学生端不再显示该轮次，已选题学生也不能继续提交。' : '开放后学生可以选择该轮次的任务。',
      okText: closing ? '关闭轮次' : '开放轮次',
      cancelText: '取消',
      okButtonProps: closing ? { danger: true } : undefined,
      onOk: async () => {
        try {
          await call('/admin/campaigns/' + campaign.id, { method: 'PATCH', body: JSON.stringify({ status: next }) }, csrf);
          message.success(closing ? '轮次已关闭' : '轮次已开放');
          await refresh({ silent: true });
        } catch (error) { message.error(error.message); throw error; }
      },
    });
  };
  const taskCount = Object.values(tasks).reduce((total, list) => total + list.length, 0);
  return <>
    <div className="admin-page-head">
      <div><span className="eyebrow">Campaigns</span><h1>轮次与任务</h1></div>
      <p>关闭后学生不能再选题或提交；已提交的材料保留。</p>
    </div>
    <div className="admin-grid">
      <section className="panel milk-panel" data-reveal>
        <header className="panel-head">
          <div><span className="eyebrow">Campaign</span><h2>创建招新轮次</h2></div>
          <span className="panel-index">01</span>
        </header>
        <Form form={campaignForm} name="campaignCreate" layout="vertical" requiredMark={false} initialValues={{ open: true }} onFinish={createCampaign}>
          <Form.Item name="name" label="轮次名称" rules={[{ required: true, whitespace: true }]}>
            <GlowInput placeholder="例如：2026 秋季招新" />
          </Form.Item>
          <Form.Item name="opens_at" label="开放时间" rules={[{ required: true }]}>
            <GlowDatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="closes_at" label="结束时间" rules={[{ required: true }]}>
            <GlowDatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="open" label="立即开放" valuePropName="checked"><Switch /></Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>创建轮次</Button>
        </Form>
      </section>
      <section className="panel milk-panel" data-reveal style={revealDelay(1)}>
        <header className="panel-head">
          <div><span className="eyebrow">Task</span><h2>创建并发布任务</h2></div>
          <span className="panel-index">02</span>
        </header>
        <Form form={taskForm} name="taskCreate" layout="vertical" requiredMark={false} initialValues={{ publish: true, plugin_config: '{}' }} onFinish={createTask}>
          <Form.Item name="campaign_id" label="招新轮次" rules={[{ required: true }]}>
            <Select placeholder="选择轮次" options={campaigns.map(x => ({ value: x.id, label: x.name + ' (' + statusLabel(x.status) + ')' }))} />
          </Form.Item>
          <Form.Item name="title" label="标题" rules={[{ required: true, whitespace: true }]}>
            <GlowInput maxLength={200} placeholder="例如：Go 后端挑战" />
          </Form.Item>
          <Form.Item name="description" label="任务说明">
            <GlowTextArea rows={3} placeholder="任务目标与交付要求" />
          </Form.Item>
          <Form.Item name="deadline_at" label="提交截止时间" rules={[{ required: true }]}>
            <GlowDatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="capacity" label="名额上限（留空不限）">
            <GlowNumber min={1} style={{ width: '100%' }} placeholder="不限" />
          </Form.Item>
          <Form.Item name="plugin_version_id" label="评测插件" rules={[{ required: true }]}>
            <Select placeholder="选择插件版本" options={plugins.filter(x => x.enabled).map(x => ({ value: x.id, label: x.name + ' v' + x.version }))} />
          </Form.Item>
          <Form.Item name="plugin_config" label="插件配置 JSON">
            <GlowTextArea rows={3} />
          </Form.Item>
          <Form.Item name="publish" label="立即发布" valuePropName="checked"><Switch /></Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>创建任务</Button>
        </Form>
      </section>
    </div>
    <section className="section">
      <SectionHead eyebrow="Campaigns" title="轮次与任务列表" hint={`${campaigns.length} 个轮次 · ${taskCount} 个任务`} />
      {!campaigns.length && <EmptyState title="还没有招新轮次" hint="用上方表单创建。" />}
      {campaigns.map((campaign, index) => {
        const list = tasks[campaign.id] || [];
        return <div className="campaign-row milk-panel" key={campaign.id} data-reveal style={revealDelay(Math.min(index, 2))}>
          <header className="campaign-row-head">
            <div className="campaign-row-title">
              <h3>{campaign.name}</h3>
              <Pill tone={statusTone(campaign.status)} dot>{statusLabel(campaign.status)}</Pill>
            </div>
            <div className="campaign-row-side">
              <span className="campaign-row-dates">{formatDate(campaign.opens_at)} — {formatDate(campaign.closes_at)}</span>
              <Button size="small" onClick={() => toggleCampaign(campaign)}>{campaign.status === 'open' ? '关闭轮次' : '开放轮次'}</Button>
            </div>
          </header>
          {list.length ? <div className="task-lines">
            {list.map(task => <div className="task-line" key={task.id}>
              <span className="task-line-title">{task.title}</span>
              <span className="task-line-meta">插件 #{task.plugin_version_id}</span>
              <span className="task-line-meta">{task.selected_count}{task.capacity != null ? ` / ${task.capacity}` : ''} 人</span>
              <Pill tone={statusTone(task.status)}>{statusLabel(task.status)}</Pill>
              <span className="task-line-actions">
                <Button size="small" type="text" onClick={() => setEditingTask({ ...task, campaign_id: campaign.id })}>编辑</Button>
                <Button size="small" type="text" onClick={() => toggleTask(task)}>{task.status === 'published' ? '关闭' : '发布'}</Button>
              </span>
            </div>)}
          </div> : <span className="muted small">暂无任务</span>}
        </div>;
      })}
    </section>
    {editingTask && <TaskEditModal task={editingTask} csrf={csrf}
      onClose={() => setEditingTask(null)}
      onSaved={async () => { setEditingTask(null); await refresh({ silent: true }); }} />}
  </>;
}

function PluginEditModal({ plugin, csrf, onClose, onSaved }) {
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const save = async values => {
    setBusy(true);
    try {
      let schema;
      try { schema = JSON.parse(values.config_schema || '{}'); }
      catch { message.error('配置 Schema 必须是合法 JSON'); return; }
      await call('/admin/plugins/' + plugin.id, { method: 'PATCH', body: JSON.stringify({
        library_path: values.library_path.trim(),
        config_schema: schema,
      }) }, csrf);
      message.success('插件已更新');
      await onSaved();
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  return <Modal open title={`编辑插件 ${plugin.name} v${plugin.version}`} okText="保存" cancelText="取消" width={560}
    confirmLoading={busy} onOk={() => form.submit()} onCancel={onClose}>
    <Form form={form} name="pluginEdit" layout="vertical" requiredMark={false} onFinish={save} initialValues={{
      library_path: plugin.library_path,
      config_schema: JSON.stringify(plugin.config_schema ?? {}, null, 2),
    }}>
      <Form.Item name="library_path" label="插件库路径（.so）" rules={[
        { required: true, whitespace: true, message: '请输入插件库路径' },
        { max: 500, message: '最多 500 个字符' },
        { pattern: /\.so$/i, message: '路径必须以 .so 结尾' },
      ]}>
        <GlowInput placeholder="plugins/example/example.so" />
      </Form.Item>
      <Form.Item name="config_schema" label="配置 Schema JSON">
        <GlowTextArea rows={5} />
      </Form.Item>
      <p className="form-hint">插件名称和版本不可修改；升级请注册新版本。修改路径后，后续任务会使用新路径。</p>
    </Form>
  </Modal>;
}

function AdminPlugins({ plugins, csrf, refresh }) {
  const { message, modal } = AntdApp.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [editingPlugin, setEditingPlugin] = useState(null);
  const source = Form.useWatch('source', form) || 'upload';
  const createPlugin = async values => {
    setBusy(true);
    try {
      let schema;
      try { schema = JSON.parse(values.config_schema || '{}'); }
      catch { message.error('配置 Schema 必须是合法 JSON'); return; }
      if (values.source === 'upload') {
        const file = values.file?.[0]?.originFileObj;
        if (!file) { message.error('请选择 .go 或 .zip 文件'); return; }
        const body = new FormData();
        body.append('name', values.name.trim());
        body.append('version', values.version.trim());
        body.append('config_schema', JSON.stringify(schema));
        body.append('enabled', String(values.enabled !== false));
        body.append('file', file);
        await call('/admin/plugins/upload', { method: 'POST', body }, csrf);
        message.success('插件已编译并注册');
      } else {
        await call('/admin/plugins', { method: 'POST', body: JSON.stringify({
          name: values.name.trim(),
          version: values.version.trim(),
          library_path: values.library_path.trim(),
          config_schema: schema,
          enabled: values.enabled,
        }) }, csrf);
        message.success('插件已注册');
      }
      form.resetFields();
      await refresh({ silent: true });
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  const toggle = plugin => {
    const next = !plugin.enabled;
    modal.confirm({
      title: next ? '启用插件？' : '停用插件？',
      content: next ? '启用后可在新任务中选择该版本。' : '停用后不能创建新任务，已有任务和评测不受影响。',
      okText: next ? '启用' : '停用',
      cancelText: '取消',
      okButtonProps: next ? undefined : { danger: true },
      onOk: async () => {
        try {
          await call('/admin/plugins/' + plugin.id, { method: 'PATCH', body: JSON.stringify({ enabled: next }) }, csrf);
          message.success(next ? '插件已启用' : '插件已停用');
          await refresh({ silent: true });
        } catch (error) { message.error(error.message); throw error; }
      },
    });
  };
  const columns = [
    {
      title: '插件', key: 'plugin',
      render: (_, row) => <div className="version-cell"><strong>{row.name}</strong><small>v{row.version}</small></div>,
    },
    {
      title: '插件库', dataIndex: 'library_path', key: 'library_path',
      render: path => <code className="schema-cell" title={path}>{path}</code>,
    },
    {
      title: '配置 Schema', dataIndex: 'config_schema', key: 'config_schema',
      render: schema => <code className="schema-cell" title={JSON.stringify(schema)}>{JSON.stringify(schema)}</code>,
    },
    {
      title: '状态', dataIndex: 'enabled', key: 'enabled',
      render: enabled => <Pill tone={enabled ? 'success' : 'neutral'} dot>{enabled ? '已启用' : '已停用'}</Pill>,
    },
    {
      title: '操作', key: 'actions',
      render: (_, row) => <div className="row-actions">
        <Button size="small" type="text" onClick={() => setEditingPlugin(row)}>编辑</Button>
        <Button size="small" danger={row.enabled} onClick={() => toggle(row)}>{row.enabled ? '停用' : '启用'}</Button>
      </div>,
    },
  ];
  const enabledCount = plugins.filter(plugin => plugin.enabled).length;
  return <>
    <div className="admin-page-head">
      <div><span className="eyebrow">Plugins</span><h1>插件管理</h1></div>
      <p>上传 .go 或 .zip 后由 API 主机编译为 .so；worker 通过 <code>plugin.Open</code> 加载，插件需与 worker 使用同一 Go 版本。</p>
    </div>
    <div className="admin-grid">
      <section className="panel milk-panel" data-reveal>
        <header className="panel-head">
          <div><span className="eyebrow">Registry</span><h2>注册插件版本</h2></div>
          <span className="panel-index">01</span>
        </header>
        <Form form={form} name="pluginCreate" layout="vertical" requiredMark={false}
          initialValues={{ enabled: true, config_schema: '{}', source: 'upload' }} onFinish={createPlugin}>
          <Form.Item name="name" label="插件名称" rules={[
            { required: true, whitespace: true, message: '请输入插件名称' },
            { max: 80, message: '最多 80 个字符' },
          ]}>
            <GlowInput placeholder="例如：code-review" />
          </Form.Item>
          <Form.Item name="version" label="版本" rules={[
            { required: true, whitespace: true, message: '请输入版本' },
            { max: 40, message: '最多 40 个字符' },
          ]}>
            <GlowInput placeholder="例如：1.0.0" />
          </Form.Item>
          <Form.Item name="source" label="插件来源">
            <Select options={[
              { value: 'upload', label: '上传源码并编译' },
              { value: 'path', label: '登记服务器上的 .so' },
            ]} />
          </Form.Item>
          {source === 'upload' ? <>
            <Form.Item name="file" label="Go 源码或压缩包" valuePropName="fileList"
              getValueFromEvent={event => (event?.fileList || []).slice(-1)}
              rules={[{
                validator: (_, value) => value && value.length
                  ? Promise.resolve()
                  : Promise.reject(new Error('请选择 .go 或 .zip 文件')),
              }]}>
              <Upload beforeUpload={() => false} maxCount={1} accept=".go,.zip">
                <Button icon={<UploadIcon />}>选择 .go 或 .zip</Button>
              </Upload>
            </Form.Item>
            <p className="form-hint">支持单个 .go 或包含 .go 文件的 .zip；非 Go 文件会被忽略，仅允许标准库与 <code>pluginapi</code>，禁止 cgo。编译后的插件在 worker 进程内运行，只上传可信代码。</p>
          </> : <Form.Item name="library_path" label="插件库路径（.so）" rules={[
            { required: true, whitespace: true, message: '请输入插件库路径' },
            { max: 500, message: '最多 500 个字符' },
            { pattern: /\.so$/i, message: '路径必须以 .so 结尾' },
          ]}>
            <GlowInput placeholder="plugins/example/example.so" />
          </Form.Item>}
          <Form.Item name="config_schema" label="配置 Schema JSON">
            <GlowTextArea rows={4} />
          </Form.Item>
          <Form.Item name="enabled" label="立即启用" valuePropName="checked"><Switch /></Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            {source === 'upload' ? '上传并编译' : '注册插件'}
          </Button>
        </Form>
      </section>
      <section className="panel milk-panel" data-reveal style={revealDelay(1)}>
        <header className="panel-head">
          <div><span className="eyebrow">Runtime</span><h2>动态加载说明</h2></div>
          <span className="panel-index">02</span>
        </header>
        <div className="settings-list">
          <div><span>已注册版本</span><strong>{plugins.length}</strong></div>
          <div><span>已启用</span><strong>{enabledCount}</strong></div>
          <div><span>已停用</span><strong>{plugins.length - enabledCount}</strong></div>
        </div>
        <p className="form-hint">worker 按任务实时读取插件路径，注册新路径无需重启。编译在 API 主机执行，需要 Go 工具链且与 worker 版本一致；替换同名 .so 后需重启 worker（Go plugin 会缓存已加载库）。插件运行在 worker 进程内并拥有其权限，只应加载可信代码。</p>
        <div className="plugin-downloads">
          <div><span>示例插件</span><small>下载包含源码与 AGENTS.md 的压缩包，或直接获取已编译版本。</small></div>
          <a className="table-link" href="/api/v1/admin/plugins/example/package">示例插件包 .zip</a>
          <a className="table-link" href="/api/v1/admin/plugins/example/binary">插件 .so</a>
        </div>
      </section>
    </div>
    <section className="section">
      <SectionHead eyebrow="Registry" title="插件版本" hint={`${plugins.length} 个`} />
      <div className="table-panel milk-panel">
        <Table rowKey="id" columns={columns} dataSource={plugins} scroll={{ x: 960 }}
          pagination={{ pageSize: 8, showSizeChanger: false, hideOnSinglePage: true }}
          locale={{ emptyText: <EmptyState title="还没有插件版本" hint="用上方表单注册。" /> }} />
      </div>
    </section>
    {editingPlugin && <PluginEditModal plugin={editingPlugin} csrf={csrf}
      onClose={() => setEditingPlugin(null)}
      onSaved={async () => { setEditingPlugin(null); await refresh({ silent: true }); }} />}
  </>;
}

function AdminPeople({ csrf, currentUserId }) {
  const { message, modal } = AntdApp.useApp();
  const [users, setUsers] = useState([]);
  const [busy, setBusy] = useState(false);
  const [query, setQuery] = useState('');
  const [role, setRole] = useState('');
  const [status, setStatus] = useState('');
  const [version, setVersion] = useState(0);
  useEffect(() => {
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      setBusy(true);
      try {
        const params = new URLSearchParams();
        if (query.trim()) params.set('q', query.trim());
        if (role) params.set('role', role);
        if (status) params.set('status', status);
        const list = await call('/admin/users' + (params.toString() ? '?' + params.toString() : ''));
        if (!cancelled) setUsers(list);
      } catch (error) { if (!cancelled) message.error(error.message); }
      finally { if (!cancelled) setBusy(false); }
    }, 250);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [query, role, status, version, message]);
  const patch = async (user, changes, successText) => {
    try {
      await call('/admin/users/' + user.id, { method: 'PATCH', body: JSON.stringify(changes) }, csrf);
      message.success(successText);
      setVersion(value => value + 1);
    } catch (error) { message.error(error.message); throw error; }
  };
  const toggleRole = user => {
    const next = user.role === 'admin' ? 'student' : 'admin';
    modal.confirm({
      title: next === 'admin' ? '设为管理员？' : '设为学生？',
      content: `将 ${user.name} 的角色改为${ROLE_TEXT[next]}，该账号现有会话会退出。`,
      okText: '确认', cancelText: '取消',
      onOk: () => patch(user, { role: next }, '角色已更新'),
    });
  };
  const toggleDisabled = user => {
    if (user.disabled) {
      patch(user, { disabled: false }, '账号已启用');
      return;
    }
    modal.confirm({
      title: '禁用账号？',
      content: `${user.name} 将立即退出，且无法再登录。`,
      okText: '禁用', cancelText: '取消', okButtonProps: { danger: true },
      onOk: () => patch(user, { disabled: true }, '账号已禁用'),
    });
  };
  const columns = [
    {
      title: '用户', dataIndex: 'name', key: 'name',
      render: (name, row) => <div className="student-cell">
        <span className="mini-avatar">{initial(name)}</span>
        <span className="student-identity"><strong>{name}</strong><small>{row.email}</small></span>
      </div>,
    },
    {
      title: '角色', dataIndex: 'role', key: 'role',
      render: value => <Pill tone={value === 'admin' ? 'ink' : 'neutral'}>{ROLE_TEXT[value] || value}</Pill>,
    },
    {
      title: '状态', dataIndex: 'disabled', key: 'disabled',
      render: disabled => <Pill tone={disabled ? 'danger' : 'success'} dot>{disabled ? '已禁用' : '正常'}</Pill>,
    },
    {
      title: '报名 / 提交', key: 'counts',
      render: (_, row) => <span className="people-counts">{row.selection_count} / {row.submission_count}</span>,
    },
    {
      title: '注册时间', dataIndex: 'created_at', key: 'created_at',
      render: value => <span className="table-muted">{formatDate(value)}</span>,
    },
    {
      title: '操作', key: 'actions',
      render: (_, row) => row.id === currentUserId
        ? <span className="table-muted">当前账号</span>
        : <div className="row-actions">
          <Button size="small" type="text" onClick={() => toggleRole(row)}>{row.role === 'admin' ? '设为学生' : '设为管理员'}</Button>
          <Button size="small" type="text" danger={!row.disabled} onClick={() => toggleDisabled(row)}>{row.disabled ? '启用' : '禁用'}</Button>
        </div>,
    },
  ];
  return <section className="section">
    <SectionHead eyebrow="People" title="人员管理" hint={busy ? '查询中…' : `${users.length} 人`} />
    <div className="people-toolbar milk-panel">
      <span className="milk-field people-search">
        <Input allowClear value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索姓名或邮箱" />
      </span>
      <Select value={role} onChange={setRole} options={[
        { value: '', label: '全部角色' },
        { value: 'student', label: '学生' },
        { value: 'admin', label: '管理员' },
      ]} />
      <Select value={status} onChange={setStatus} options={[
        { value: '', label: '全部状态' },
        { value: 'active', label: '正常' },
        { value: 'disabled', label: '已禁用' },
      ]} />
    </div>
    <div className="table-panel milk-panel">
      <Table rowKey="id" loading={busy} columns={columns} dataSource={users} scroll={{ x: 980 }}
        pagination={{ pageSize: 10, showSizeChanger: false, hideOnSinglePage: true }}
        locale={{ emptyText: <EmptyState title="没有匹配的用户" hint="调整搜索或筛选条件。" /> }} />
    </div>
  </section>;
}

function AdminSubmissions({ submissions, csrf, refresh }) {
  const { message } = AntdApp.useApp();
  const retry = async id => {
    try { await call('/admin/evaluations/' + id + '/retry', { method: 'POST' }, csrf); message.success('已重新排队'); await refresh({ silent: true }); }
    catch (error) { message.error(error.message); }
  };
  const columns = [
    {
      title: '同学', dataIndex: 'student_name', key: 'student_name',
      render: (name, row) => <div className="student-cell">
        <span className="mini-avatar">{initial(name)}</span>
        <span className="student-identity"><strong>{name}</strong><small>{row.student_email}</small></span>
      </div>,
    },
    { title: '任务', dataIndex: 'task_title', key: 'task_title' },
    {
      title: '版本', dataIndex: 'version', key: 'version',
      render: (version, row) => <div className="version-cell"><strong>v{version}</strong><small>{formatDateTime(row.submitted_at)}</small></div>,
    },
    {
      title: '评测', dataIndex: 'evaluation_status', key: 'evaluation_status',
      render: (status, row) => <div className="evaluation-cell">
        <Pill tone={statusTone(status)} dot>{statusLabel(status)}</Pill>
        {row.error && <div className="evaluation-error">{row.error}</div>}
      </div>,
    },
    {
      title: '分数', dataIndex: 'score', key: 'score',
      render: score => <span className={`score-cell${score === null || score === undefined ? ' is-empty' : ''}`}>
        {score === null || score === undefined ? '—' : formatScore(score)}
      </span>,
    },
    {
      title: '材料', key: 'files',
      render: (_, row) => <div className="file-links">
        <a className="table-link" href={'/api/v1/files/' + row.resume_file_id + '/download'}>简历</a>
        <a className="table-link" href={'/api/v1/files/' + row.package_file_id + '/download'}>作品</a>
      </div>,
    },
    {
      title: '操作', key: 'action',
      render: (_, row) => row.evaluation_status === 'failed'
        ? <Button size="small" onClick={() => retry(row.evaluation_job_id)}>重新评测</Button>
        : <span className="table-muted">—</span>,
    },
  ];
  return <section className="section">
    <SectionHead eyebrow="Submissions" title="提交与评测" hint={`共 ${submissions.length} 条记录`} />
    <div className="table-panel milk-panel">
      <Table rowKey="id" columns={columns} dataSource={submissions} scroll={{ x: 940 }}
        pagination={{ pageSize: 8, showSizeChanger: false, hideOnSinglePage: true }}
        locale={{ emptyText: <EmptyState title="还没有提交" hint="学生提交后会显示在这里。" /> }} />
    </div>
  </section>;
}

function SettingsEditor({ settings, csrf, refresh, onSaved }) {
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const save = async values => {
    setBusy(true);
    try {
      const updated = await call('/admin/settings', { method: 'PATCH', body: JSON.stringify({
        site_name: values.site_name.trim(),
        season_year: String(values.season_year),
      }) }, csrf);
      onSaved(updated);
      form.setFieldsValue({
        site_name: updated.site_name,
        season_year: Number(updated.season_year),
      });
      message.success('基础设置已保存');
      await refresh({ silent: true });
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  return <section className="panel milk-panel settings-editor" data-reveal>
    <header className="panel-head">
      <div><span className="eyebrow">Site</span><h2>站点设置</h2></div>
      <span className="panel-index">SET</span>
    </header>
    <Form form={form} name="siteSettings" layout="vertical" requiredMark={false} onFinish={save} initialValues={{
      site_name: settings.site_name,
      season_year: Number(settings.season_year),
    }}>
      <div className="settings-form-grid">
        <Form.Item name="site_name" label="站点名称" rules={[
          { required: true, whitespace: true, message: '请输入站点名称' },
          { max: 40, message: '最多 40 个字符' },
        ]}>
          <GlowInput maxLength={40} />
        </Form.Item>
        <Form.Item name="season_year" label="招新年份" rules={[
          { required: true, message: '请输入年份' },
          { type: 'number', min: 2000, max: 2100, message: '年份范围为 2000–2100' },
        ]}>
          <GlowNumber min={2000} max={2100} precision={0} style={{ width: '100%' }} />
        </Form.Item>
      </div>
      <p className="form-hint">站点名称与年份会同步到页头、登录页和页脚。</p>
      <Button type="primary" htmlType="submit" loading={busy}>保存设置</Button>
    </Form>
  </section>;
}

function MailSettingsEditor({ settings, csrf, refresh, onSaved }) {
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const save = async values => {
    setBusy(true);
    try {
      const body = {
        registration_enabled: values.registration_enabled,
        email_dev_mode: values.email_dev_mode,
        smtp_host: values.smtp_host || '',
        smtp_port: values.smtp_port == null || values.smtp_port === '' ? '' : String(values.smtp_port),
        smtp_username: values.smtp_username || '',
        smtp_from: values.smtp_from || '',
      };
      if (values.smtp_password) body.smtp_password = values.smtp_password;
      if (values.clear_smtp_password) body.clear_smtp_password = true;
      const updated = await call('/admin/settings', { method: 'PATCH', body: JSON.stringify(body) }, csrf);
      onSaved(updated);
      form.setFieldsValue({
        registration_enabled: updated.registration_enabled,
        email_dev_mode: updated.email_dev_mode,
        smtp_host: updated.smtp_host || '',
        smtp_port: updated.smtp_port || undefined,
        smtp_username: updated.smtp_username || '',
        smtp_from: updated.smtp_from || '',
        smtp_password: '',
        clear_smtp_password: false,
      });
      message.success('邮件设置已保存');
      await refresh({ silent: true });
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  return <section className="panel milk-panel settings-editor" data-reveal>
    <header className="panel-head">
      <div><span className="eyebrow">Mail & Registration</span><h2>邮件与注册</h2></div>
      <span className="panel-index">MAIL</span>
    </header>
    <Form form={form} name="mailSettings" layout="vertical" requiredMark={false} onFinish={save} initialValues={{
      registration_enabled: settings.registration_enabled,
      email_dev_mode: settings.email_dev_mode,
      smtp_host: settings.smtp_host || '',
      smtp_port: settings.smtp_port || undefined,
      smtp_username: settings.smtp_username || '',
      smtp_from: settings.smtp_from || '',
    }}>
      <Form.Item name="registration_enabled" label="开放学生注册" valuePropName="checked"><Switch /></Form.Item>
      <Form.Item name="email_dev_mode" label="开发模式：不发送真实邮件，验证码写入日志并显示在页面" valuePropName="checked">
        <Switch />
      </Form.Item>
      <div className="settings-form-grid">
        <Form.Item name="smtp_host" label="SMTP 主机"><GlowInput placeholder="smtp.example.org" /></Form.Item>
        <Form.Item name="smtp_port" label="SMTP 端口" rules={[
          { type: 'number', min: 1, max: 65535, message: '端口范围为 1–65535' },
        ]}>
          <GlowNumber min={1} max={65535} precision={0} style={{ width: '100%' }} placeholder="587" />
        </Form.Item>
      </div>
      <div className="settings-form-grid">
        <Form.Item name="smtp_username" label="SMTP 用户名"><GlowInput autoComplete="off" placeholder="可选" /></Form.Item>
        <Form.Item name="smtp_password" label="SMTP 密码">
          <GlowPassword autoComplete="new-password" placeholder={settings.smtp_password_set ? '已配置，留空不修改' : '未设置'} />
        </Form.Item>
      </div>
      <Form.Item name="smtp_from" label="发件人"><GlowInput placeholder="no-reply@example.org" /></Form.Item>
      {settings.smtp_password_saved && <Form.Item name="clear_smtp_password" valuePropName="checked">
        <Checkbox>清除已保存的 SMTP 密码</Checkbox>
      </Form.Item>}
      <p className="form-hint">留空的主机、端口、用户名和发件人会回退到服务器环境变量。密码保存后不会再显示；SMTP 密码以明文保存在数据库中，请保护数据库备份。</p>
      <Button type="primary" htmlType="submit" loading={busy}>保存邮件设置</Button>
    </Form>
  </section>;
}

function AdminSettings({ csrf, refresh, plugins }) {
  const { message } = AntdApp.useApp();
  const [settings, setSettings] = useState(null);
  const [busy, setBusy] = useState(false);
  const load = useCallback(async () => {
    setBusy(true);
    try { setSettings(await call('/admin/settings')); }
    catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  }, [message]);
  useEffect(() => { load(); }, [load]);
  return <section className="section">
    <SectionHead eyebrow="Settings" title="基础设置" hint={busy ? '读取中…' : '站点与运行信息'} />
    {!settings
      ? <div className="settings-loading milk-panel"><Spin /><span>正在读取设置</span></div>
      : <>
        <SettingsEditor settings={settings} csrf={csrf} refresh={refresh}
          onSaved={updated => setSettings(previous => ({ ...previous, ...updated }))} />
        <MailSettingsEditor settings={settings} csrf={csrf} refresh={refresh}
          onSaved={updated => setSettings(previous => ({ ...previous, ...updated }))} />
        <div className="settings-grid">
          <section className="settings-card milk-panel">
            <span className="eyebrow">Runtime</span>
            <h3>运行环境</h3>
            <div className="settings-list">
              <div><span>系统版本</span><strong>v{settings.version}</strong></div>
              <div><span>Go</span><strong>{settings.go_version}</strong></div>
              <div><span>数据库</span><strong>{settings.database_version}</strong></div>
              <div><span>数据库时间</span><strong>{formatDateTime(settings.database_time)}</strong></div>
              <div><span>插件超时</span><strong>{settings.plugin_timeout}</strong></div>
            </div>
          </section>
          <section className="settings-card milk-panel">
            <span className="eyebrow">Data</span>
            <h3>数据概览</h3>
            <div className="settings-list">
              <div><span>学生 / 管理员</span><strong>{settings.counts.students} / {settings.counts.admins}</strong></div>
              <div><span>禁用账号</span><strong>{settings.counts.disabled_users}</strong></div>
              <div><span>开放轮次 / 已发布任务</span><strong>{settings.counts.open_campaigns} / {settings.counts.published_tasks}</strong></div>
              <div><span>提交 / 失败评测</span><strong>{settings.counts.submissions} / {settings.counts.failed_evaluations}</strong></div>
            </div>
          </section>
          <section className="settings-card milk-panel">
            <span className="eyebrow">Plugins</span>
            <h3>评测插件</h3>
            {plugins.length
              ? <div className="settings-list">
                {plugins.map(plugin => <div key={plugin.id}>
                  <span>{plugin.name} v{plugin.version}{plugin.enabled ? '' : '（已停用）'}</span>
                  <strong className="settings-image" title={plugin.library_path}>{plugin.library_path}</strong>
                </div>)}
              </div>
              : <p className="muted small">暂无启用插件。</p>}
          </section>
        </div>
      </>}
  </section>;
}

function Admin({ me, site, campaigns, tasks, plugins, submissions, csrf, refresh }) {
  const [tab, setTab] = useState('overview');
  const rootRef = useRef(null);
  useReveal(rootRef);
  return <div className="admin-layout" ref={rootRef}>
    <aside className="admin-sidebar milk-panel">
      <div className="admin-sidebar-head">
        <span className="eyebrow">Admin</span>
        <strong>管理台</strong>
        <small>{me.name}</small>
      </div>
      <nav className="admin-nav" aria-label="管理台导航">
        {ADMIN_TABS.map(item => <button type="button" key={item.id}
          className={`admin-nav-item${tab === item.id ? ' is-active' : ''}`}
          aria-current={tab === item.id ? 'page' : undefined}
          onClick={() => setTab(item.id)}>{item.label}</button>)}
      </nav>
      <div className="admin-sidebar-foot">v{site.version}</div>
    </aside>
    <div className="admin-content">
      {tab === 'overview' && <AdminOverview campaigns={campaigns} tasks={tasks} submissions={submissions} onOpenTab={setTab} />}
      {tab === 'tasks' && <AdminTasks campaigns={campaigns} tasks={tasks} plugins={plugins} csrf={csrf} refresh={refresh} />}
      {tab === 'plugins' && <AdminPlugins plugins={plugins} csrf={csrf} refresh={refresh} />}
      {tab === 'people' && <AdminPeople csrf={csrf} currentUserId={me.id} />}
      {tab === 'submissions' && <AdminSubmissions submissions={submissions} csrf={csrf} refresh={refresh} />}
      {tab === 'settings' && <AdminSettings csrf={csrf} refresh={refresh} plugins={plugins} />}
    </div>
  </div>;
}

export default function App() {
  const { message } = AntdApp.useApp();
  const [loading, setLoading] = useState(true);
  const [me, setMe] = useState(null);
  const [site, setSite] = useState(DEFAULT_SITE);
  const [campaigns, setCampaigns] = useState([]);
  const [tasks, setTasks] = useState({});
  const [selections, setSelections] = useState([]);
  const [plugins, setPlugins] = useState([]);
  const [submissions, setSubmissions] = useState([]);
  const refresh = useCallback(async (options = {}) => {
    const silent = options?.silent === true;
    if (!silent) setLoading(true);
    try {
      try { setSite({ ...DEFAULT_SITE, ...(await call('/config')) }); } catch { /* older API or transient failure */ }
      const user = await call('/me');
      setMe(user);
      const [campaignList, selected, pluginList, submitted] = await Promise.all([
        call('/campaigns'),
        user.role === 'student' ? call('/me/selections') : Promise.resolve([]),
        user.role === 'admin' ? call('/admin/plugins?all=true') : Promise.resolve([]),
        user.role === 'admin' ? call('/admin/submissions') : Promise.resolve([]),
      ]);
      const taskPairs = await Promise.all(campaignList.map(async campaign => [campaign.id, await call('/campaigns/' + campaign.id + '/tasks')]));
      setCampaigns(campaignList); setTasks(Object.fromEntries(taskPairs));
      setSelections(selected); setPlugins(pluginList); setSubmissions(submitted);
    } catch (error) {
      if (error.status === 401) setMe(null);
      else message.error(error.message);
    } finally { if (!silent) setLoading(false); }
  }, [message]);
  useEffect(() => { refresh(); }, [refresh]);
  useEffect(() => { document.title = `${site.site_name} 招新系统`; }, [site.site_name]);
  const logout = async () => {
    try { await call('/auth/logout', { method: 'POST' }, me.csrf_token); setMe(null); message.success('已退出'); }
    catch (error) { message.error(error.message); }
  };
  return <div className="app-shell">
    <header className="site-header">
      <div className="header-inner milk-panel">
        <div className="brand">
          <BrandMark />
          <span className="brand-text">
            <span className="brand-name">{site.site_name}</span>
            <span className="brand-sub">RECRUIT SYSTEM</span>
          </span>
        </div>
        {me ? <div className="header-user">
          <span className="avatar">{initial(me.name)}</span>
          <span className="header-identity">
            <strong>{me.name}</strong>
            <span>{ROLE_TEXT[me.role] || me.role}工作台</span>
          </span>
          <Button type="text" className="logout-button" onClick={logout}>退出登录</Button>
        </div> : <span className="header-tagline">{site.season_year} 招新季</span>}
      </div>
    </header>
    <main className="page">
      {loading
        ? <div className="loading-state"><Spin size="large" /><span>正在加载工作台</span></div>
        : !me
          ? <Auth onSignedIn={refresh} site={site} />
          : me.role === 'admin'
            ? <Admin me={me} site={site} campaigns={campaigns} tasks={tasks} plugins={plugins} submissions={submissions} csrf={me.csrf_token} refresh={refresh} />
            : <Student name={me.name} campaigns={campaigns} tasks={tasks} selections={selections} csrf={me.csrf_token} refresh={refresh} />}
    </main>
    <footer className="site-footer">
      <div className="footer-inner">
        <span>© {site.season_year} {site.site_name} Recruit</span>
        <span>v{site.version}</span>
      </div>
    </footer>
  </div>;
}
