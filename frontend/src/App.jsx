import { useCallback, useEffect, useState } from 'react';
import {
  Alert, App as AntdApp, Button, Card, DatePicker, Empty, Form, Input, InputNumber,
  Layout, Select, Space, Spin, Switch, Table, Tabs, Tag, Typography, Upload,
} from 'antd';

const { Title, Text, Paragraph } = Typography;

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

function Auth({ onSignedIn }) {
  const { message } = AntdApp.useApp();
  const [busy, setBusy] = useState(false);
  const submit = async (path, values) => {
    setBusy(true);
    try {
      await call(path, { method: 'POST', body: JSON.stringify(values) });
      message.success('欢迎使用 Birkenwald 招新系统');
      await onSignedIn();
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  return <div className="auth-grid">
    <Card title="登录">
      <Form layout="vertical" onFinish={values => submit('/auth/login', values)}>
        <Form.Item label="邮箱" name="email" rules={[{ required: true, type: 'email' }]}><Input autoComplete="email" /></Form.Item>
        <Form.Item label="密码" name="password" rules={[{ required: true }]}><Input.Password autoComplete="current-password" /></Form.Item>
        <Button type="primary" htmlType="submit" loading={busy}>登录</Button>
      </Form>
    </Card>
    <Card title="学生注册">
      <Form layout="vertical" onFinish={values => submit('/auth/register', values)}>
        <Form.Item label="姓名" name="name" rules={[{ required: true }]}><Input maxLength={100} /></Form.Item>
        <Form.Item label="邮箱" name="email" rules={[{ required: true, type: 'email' }]}><Input autoComplete="email" /></Form.Item>
        <Form.Item label="密码（至少 12 位）" name="password" rules={[{ required: true, min: 12 }]}><Input.Password autoComplete="new-password" /></Form.Item>
        <Button type="primary" htmlType="submit" loading={busy}>注册</Button>
      </Form>
    </Card>
  </div>;
}

function SelectionCard({ item, csrf, refresh }) {
  const { message } = AntdApp.useApp();
  const [resume, setResume] = useState([]);
  const [pack, setPack] = useState([]);
  const [busy, setBusy] = useState(false);
  const uploadProps = (files, setFiles, accept) => ({
    accept, maxCount: 1, fileList: files,
    beforeUpload: () => false,
    onChange: ({ fileList }) => setFiles(fileList.slice(-1)),
  });
  const uploadFile = async (file, kind) => {
    const body = new FormData();
    body.append('kind', kind);
    body.append('file', file.originFileObj || file);
    return call('/files', { method: 'POST', body }, csrf);
  };
  const submit = async () => {
    if (!resume.length || !pack.length) { message.warning('请同时选择简历 PDF 和作品 ZIP'); return; }
    setBusy(true);
    try {
      const [r, p] = await Promise.all([uploadFile(resume[0], 'resume'), uploadFile(pack[0], 'package')]);
      await call('/selections/' + item.id + '/submissions', {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
        body: JSON.stringify({ resume_file_id: r.id, package_file_id: p.id }),
      }, csrf);
      message.success('提交成功，已进入评测队列');
      setResume([]); setPack([]);
      await refresh();
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  return <Card className="section-card" title={item.task_title}>
    {item.submission_id ? <Space wrap>
      <Tag color="blue">第 {item.version} 版</Tag><Tag>{item.evaluation_status || item.submission_status}</Tag>
      {item.score !== null && <Tag color="green">得分 {item.score}</Tag>}
    </Space> : <Text type="secondary">尚未提交作品</Text>}
    {item.error && <Alert className="spaced" type="error" message={item.error} showIcon />}
    <div className="spaced"><Text strong>简历 PDF</Text><Upload {...uploadProps(resume, setResume, '.pdf,application/pdf')}><Button>选择简历</Button></Upload></div>
    <div className="spaced"><Text strong>作品 ZIP</Text><Upload {...uploadProps(pack, setPack, '.zip,application/zip')}><Button>选择作品包</Button></Upload></div>
    <Button className="spaced" type="primary" loading={busy} onClick={submit}>{item.submission_id ? '提交新版本' : '上传并提交'}</Button>
  </Card>;
}

function Student({ campaigns, tasks, selections, csrf, refresh }) {
  const { message } = AntdApp.useApp();
  const [selecting, setSelecting] = useState(null);
  const chosen = new Set(selections.map(item => item.campaign_id));
  const selectTask = async (campaignID, taskID) => {
    setSelecting(taskID);
    try {
      await call('/campaigns/' + campaignID + '/selections', { method: 'POST', body: JSON.stringify({ task_id: taskID }) }, csrf);
      message.success('任务选择成功'); await refresh();
    } catch (error) { message.error(error.message); }
    finally { setSelecting(null); }
  };
  return <>
    <Title level={3}>开放任务</Title>
    {!campaigns.length && <Empty description="目前没有开放的招新轮次" />}
    {campaigns.map(campaign => <Card key={campaign.id} className="section-card" title={campaign.name}>
      <Paragraph type="secondary">开放至 {new Date(campaign.closes_at).toLocaleString()}</Paragraph>
      <div className="task-grid">
        {(tasks[campaign.id] || []).map(task => <Card key={task.id} size="small" title={task.title}>
          <Paragraph>{task.description || '暂无说明'}</Paragraph>
          <Paragraph type="secondary">截止：{new Date(task.deadline_at).toLocaleString()}</Paragraph>
          <Paragraph type="secondary">名额：{task.capacity === null ? '不限' : task.selected_count + ' / ' + task.capacity}</Paragraph>
          <Button type="primary" disabled={chosen.has(campaign.id) || (task.capacity !== null && task.selected_count >= task.capacity)}
            loading={selecting === task.id} onClick={() => selectTask(campaign.id, task.id)}>
            {chosen.has(campaign.id) ? '本轮已选任务' : '选择任务'}
          </Button>
        </Card>)}
      </div>
      {!(tasks[campaign.id] || []).length && <Empty description="暂无已发布任务" />}
    </Card>)}
    <Title level={3}>我的报名与提交</Title>
    {!selections.length && <Empty description="尚未选择任务" />}
    {selections.map(item => <SelectionCard key={item.id} item={item} csrf={csrf} refresh={refresh} />)}
  </>;
}

function Admin({ campaigns, tasks, plugins, submissions, csrf, refresh }) {
  const { message } = AntdApp.useApp();
  const [campaignForm] = Form.useForm();
  const [taskForm] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const createCampaign = async values => {
    setBusy(true);
    try {
      await call('/admin/campaigns', { method: 'POST', body: JSON.stringify({
        name: values.name, opens_at: values.opens_at.toISOString(), closes_at: values.closes_at.toISOString(), open: values.open,
      }) }, csrf);
      campaignForm.resetFields(); message.success('轮次已创建'); await refresh();
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
      taskForm.resetFields(); message.success('任务已创建'); await refresh();
    } catch (error) { message.error(error.message); }
    finally { setBusy(false); }
  };
  const retry = async id => {
    try { await call('/admin/evaluations/' + id + '/retry', { method: 'POST' }, csrf); message.success('评测已重新排队'); await refresh(); }
    catch (error) { message.error(error.message); }
  };
  const columns = [
    { title: '同学', dataIndex: 'student_name', key: 'student_name', render: (name, row) => <><div>{name}</div><Text type="secondary">{row.student_email}</Text></> },
    { title: '任务', dataIndex: 'task_title', key: 'task_title' },
    { title: '版本', dataIndex: 'version', key: 'version' },
    { title: '评测', dataIndex: 'evaluation_status', key: 'evaluation_status', render: (status, row) => <><Tag color={status === 'succeeded' ? 'green' : status === 'failed' ? 'red' : 'blue'}>{status}</Tag>{row.error && <div className="muted">{row.error}</div>}</> },
    { title: '分数', dataIndex: 'score', key: 'score', render: score => score === null ? '—' : score },
    { title: '材料', key: 'files', render: (_, row) => <div className="file-links"><a href={'/api/v1/files/' + row.resume_file_id + '/download'}>简历</a><a href={'/api/v1/files/' + row.package_file_id + '/download'}>作品</a></div> },
    { title: '操作', key: 'action', render: (_, row) => row.evaluation_status === 'failed' ? <Button size="small" onClick={() => retry(row.evaluation_job_id)}>重新评测</Button> : null },
  ];
  return <>
    <div className="admin-grid">
      <Card title="创建招新轮次" className="section-card">
        <Form form={campaignForm} layout="vertical" initialValues={{ open: true }} onFinish={createCampaign}>
          <Form.Item name="name" label="轮次名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="opens_at" label="开放时间" rules={[{ required: true }]}><DatePicker showTime style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="closes_at" label="结束时间" rules={[{ required: true }]}><DatePicker showTime style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="open" label="立即开放" valuePropName="checked"><Switch /></Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>创建轮次</Button>
        </Form>
      </Card>
      <Card title="创建并发布任务" className="section-card">
        <Form form={taskForm} layout="vertical" initialValues={{ publish: true, plugin_config: '{}' }} onFinish={createTask}>
          <Form.Item name="campaign_id" label="招新轮次" rules={[{ required: true }]}><Select options={campaigns.map(x => ({ value: x.id, label: x.name + ' (' + x.status + ')' }))} /></Form.Item>
          <Form.Item name="title" label="标题" rules={[{ required: true }]}><Input maxLength={200} /></Form.Item>
          <Form.Item name="description" label="任务说明"><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="deadline_at" label="提交截止时间" rules={[{ required: true }]}><DatePicker showTime style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="capacity" label="名额上限（留空不限）"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="plugin_version_id" label="评测插件" rules={[{ required: true }]}><Select options={plugins.map(x => ({ value: x.id, label: x.name + ' v' + x.version }))} /></Form.Item>
          <Form.Item name="plugin_config" label="插件配置 JSON"><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="publish" label="立即发布" valuePropName="checked"><Switch /></Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>创建任务</Button>
        </Form>
      </Card>
    </div>
    <Title level={3}>已创建任务</Title>
    {campaigns.map(campaign => <Card key={campaign.id} className="section-card" title={campaign.name}>
      {(tasks[campaign.id] || []).map(task => <Paragraph key={task.id}><Tag>{task.status}</Tag>{task.title} · 插件 #{task.plugin_version_id} · 已选 {task.selected_count}</Paragraph>)}
      {!(tasks[campaign.id] || []).length && <Text type="secondary">暂无任务</Text>}
    </Card>)}
    <Title level={3}>提交与评测</Title>
    <Card><Table rowKey="id" columns={columns} dataSource={submissions} scroll={{ x: 900 }} pagination={{ pageSize: 10 }} /></Card>
  </>;
}

export default function App() {
  const { message } = AntdApp.useApp();
  const [loading, setLoading] = useState(true);
  const [me, setMe] = useState(null);
  const [campaigns, setCampaigns] = useState([]);
  const [tasks, setTasks] = useState({});
  const [selections, setSelections] = useState([]);
  const [plugins, setPlugins] = useState([]);
  const [submissions, setSubmissions] = useState([]);
  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const user = await call('/me');
      setMe(user);
      const [campaignList, selected, pluginList, submitted] = await Promise.all([
        call('/campaigns'),
        user.role === 'student' ? call('/me/selections') : Promise.resolve([]),
        user.role === 'admin' ? call('/admin/plugins') : Promise.resolve([]),
        user.role === 'admin' ? call('/admin/submissions') : Promise.resolve([]),
      ]);
      const taskPairs = await Promise.all(campaignList.map(async campaign => [campaign.id, await call('/campaigns/' + campaign.id + '/tasks')]));
      setCampaigns(campaignList); setTasks(Object.fromEntries(taskPairs));
      setSelections(selected); setPlugins(pluginList); setSubmissions(submitted);
    } catch (error) {
      if (error.status === 401) setMe(null);
      else message.error(error.message);
    } finally { setLoading(false); }
  }, [message]);
  useEffect(() => { refresh(); }, [refresh]);
  const logout = async () => {
    try { await call('/auth/logout', { method: 'POST' }, me.csrf_token); setMe(null); message.success('已退出'); }
    catch (error) { message.error(error.message); }
  };
  return <Layout>
    <header className="site-header"><h1>Birkenwald 招新系统</h1><small>v0.1.0 · React + Ant Design</small></header>
    <main className="page">
      {loading ? <Spin size="large" /> : !me ? <Auth onSignedIn={refresh} /> : <>
        <div className="header-row"><Title level={2} style={{ margin: 0 }}>{me.name} <Text type="secondary" style={{ fontSize: 16 }}>{me.role === 'admin' ? '管理员' : '学生'}</Text></Title><Button onClick={logout}>退出登录</Button></div>
        {me.role === 'admin' ? <Admin campaigns={campaigns} tasks={tasks} plugins={plugins} submissions={submissions} csrf={me.csrf_token} refresh={refresh} />
          : <Student campaigns={campaigns} tasks={tasks} selections={selections} csrf={me.csrf_token} refresh={refresh} />}
      </>}
    </main>
  </Layout>;
}
