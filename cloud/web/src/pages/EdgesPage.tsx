import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy, Edit3, KeyRound, Plus } from 'lucide-react'
import { FormEvent, useMemo, useState } from 'react'
import { Link, NavLink, useParams } from 'react-router'
import { APIError, protoSend } from '../api'
import { CertificateState } from './CertificatesPage'
import { compactID, dateTime } from '../format'
import { BindCertificateProfileRequestSchema, BindCertificateProfileResponseSchema, ListCertificateProfilesResponseSchema } from '../generated/cloud/v1/certificate_pb'
import { CreateEdgeRequestSchema, CreateEdgeResponseSchema, ListEdgesResponseSchema, UpdateEdgeRequestSchema, UpdateEdgeResponseSchema, type ManagedEdge } from '../generated/cloud/v1/edge_config_pb'
import { ListDaemonsResponseSchema } from '../generated/cloud/v1/enrollment_pb'
import { ListRuntimeSessionsResponseSchema } from '../generated/cloud/v1/operator_pb'
import { useProtoQuery } from '../query'
import { Button, Dialog, Empty, ErrorState, Field, Input, Notice, PageHeader, Skeleton, Status, TableFrame } from '../ui'

type EdgeForm = { name: string; region: string; capacity: string; publicEndpoint: string }
const blank: EdgeForm = { name: '', region: '', capacity: '1000', publicEndpoint: '' }

export function EdgesPage() {
  const query = useProtoQuery(['edges'], '/api/operator/edges', ListEdgesResponseSchema, 30_000)
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<EdgeForm>(blank)
  const [install, setInstall] = useState('')
  const createEdge = useMutation({
    mutationFn: () => protoSend('/api/operator/edges', CreateEdgeRequestSchema, create(CreateEdgeRequestSchema, { ...form, capacity: BigInt(form.capacity || 0) }), CreateEdgeResponseSchema),
    onSuccess: (result) => { setInstall(result.installCommand); setForm(blank); void client.invalidateQueries({ queryKey: ['edges'] }) },
  })
  function submit(event: FormEvent) { event.preventDefault(); createEdge.mutate() }
  return <>
    <PageHeader title="Edge 管理" meta="配置来自数据库，在线状态来自 Controller 内存目录" actions={<Button tone="primary" onClick={() => { setInstall(''); setOpen(true) }}><Plus size={17} />添加 Edge</Button>} />
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : !query.data?.edges.length ? <Empty>尚未创建 Edge</Empty> : <EdgeTable edges={query.data.edges} />}
    <Dialog title={install ? '安装命令' : '添加 Edge'} open={open} onClose={() => setOpen(false)} closable={!createEdge.isPending} footer={install ? <Button tone="primary" onClick={() => setOpen(false)}>完成</Button> : <><Button tone="quiet" onClick={() => setOpen(false)} disabled={createEdge.isPending}>取消</Button><Button tone="primary" type="submit" form="create-edge" disabled={createEdge.isPending}>生成安装命令</Button></>}>
      {install ? <><Notice tone="warning">命令中的注册凭据只显示一次，并在十分钟后失效。</Notice><div className="command-box"><code>{install}</code><Button onClick={() => navigator.clipboard.writeText(install)}><Copy size={16} />复制</Button></div></> : <form id="create-edge" className="form-grid" onSubmit={submit}>
        <Field label="名称"><Input required value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field>
        <Field label="区域"><Input required placeholder="例如 cn-north-1" value={form.region} onChange={(event) => setForm({ ...form, region: event.target.value })} /></Field>
        <Field label="域名或域名:端口"><Input required placeholder="edge.example.com:443" value={form.publicEndpoint} onChange={(event) => setForm({ ...form, publicEndpoint: event.target.value })} /></Field>
        <Field label="daemon 容量"><Input required min="1" type="number" value={form.capacity} onChange={(event) => setForm({ ...form, capacity: event.target.value })} /></Field>
        {createEdge.error && <p className="form-error" role="alert">无法创建 Edge，请检查配置后重试。{createEdge.error instanceof APIError && createEdge.error.correlationID ? ` 关联 ID：${createEdge.error.correlationID}` : ''}</p>}
      </form>}
    </Dialog>
  </>
}

function EdgeTable({ edges }: { edges: ManagedEdge[] }) {
  return <TableFrame><table><thead><tr><th>Edge</th><th>入口</th><th>区域</th><th>状态</th><th>证书</th><th>负载</th><th>版本</th><th>最后上报</th><th /></tr></thead><tbody>{edges.map((edge) => <tr key={edge.config?.edgeId}>
    <td><strong>{edge.config?.name}</strong><small className="mono">{compactID(edge.config?.edgeId ?? '')}</small></td>
    <td className="mono">{edge.config?.publicEndpoint}</td><td>{edge.config?.region}</td>
    <td><Status active={Boolean(edge.runtime?.online)}>{edge.runtime?.online ? '在线' : edge.config?.enabled ? '离线' : '已停用'}</Status></td><td><CertificateState state={edge.certificate?.syncState} online={edge.runtime?.online} /><small>{edge.certificate?.certificateProfileName || '-'}</small></td>
    <td>{edge.runtime?.agentCount.toString() ?? '0'} / {edge.config?.capacity.toString() ?? '0'}<small>{edge.runtime?.sessionCount.toString() ?? '0'} 个会话</small></td>
    <td>{edge.runtime?.softwareVersion || '-'}</td><td>{dateTime(edge.runtime?.lastHeartbeat)}</td>
    <td><Link className="table-link" to={`/app/admin/edges/${edge.config?.edgeId}/overview`}>查看</Link></td>
  </tr>)}</tbody></table></TableFrame>
}

const tabs = [
  ['overview', '概览'], ['daemons', '已注册 daemon'], ['connections', '实时连接'], ['certificates', '证书'], ['settings', '配置与审计'],
] as const

export function EdgeDetailPage() {
  const { edgeId = '', tab = 'overview' } = useParams()
  const edges = useProtoQuery(['edges'], '/api/operator/edges', ListEdgesResponseSchema, 30_000)
  const edge = edges.data?.edges.find((item) => item.config?.edgeId === edgeId)
  if (edges.isPending) return <Skeleton />
  if (edges.error) return <ErrorState error={edges.error} onRetry={() => void edges.refetch()} />
  if (!edge) return <Empty>Edge 不存在</Empty>
  return <>
    <PageHeader title={edge.config?.name || 'Edge 详情'} meta={`${edge.config?.region} · ${edge.config?.publicEndpoint}`} />
    <nav className="tabs" aria-label="Edge 详情">{tabs.map(([value, label]) => <NavLink key={value} to={`/app/admin/edges/${edgeId}/${value}`}>{label}</NavLink>)}</nav>
    {tab === 'overview' && <EdgeOverview edge={edge} />}
    {tab === 'daemons' && <EdgeDaemons edgeId={edgeId} />}
    {tab === 'connections' && <EdgeConnections edgeId={edgeId} />}
    {tab === 'certificates' && <EdgeCertificate edge={edge} />}
    {tab === 'settings' && <EdgeSettings edge={edge} />}
  </>
}

function EdgeOverview({ edge }: { edge: ManagedEdge }) {
  return <div className="detail-columns"><section className="plain-section"><header><h2>基础配置</h2></header><dl className="detail-list"><div><dt>Edge ID</dt><dd className="mono">{edge.config?.edgeId}</dd></div><div><dt>区域</dt><dd>{edge.config?.region}</dd></div><div><dt>公网入口</dt><dd className="mono">{edge.config?.publicEndpoint}</dd></div><div><dt>标称容量</dt><dd>{edge.config?.capacity.toString()} daemon</dd></div><div><dt>配置版本</dt><dd>v{edge.config?.version.toString()}</dd></div></dl></section><section className="plain-section"><header><h2>当前运行状态</h2></header><dl className="detail-list"><div><dt>连接</dt><dd><Status active={Boolean(edge.runtime?.online)}>{edge.runtime?.online ? '在线' : '离线'}</Status></dd></div><div><dt>软件版本</dt><dd>{edge.runtime?.softwareVersion || '-'}</dd></div><div><dt>daemon</dt><dd>{edge.runtime?.agentCount.toString() ?? '0'}</dd></div><div><dt>客户端会话</dt><dd>{edge.runtime?.sessionCount.toString() ?? '0'}</dd></div><div><dt>最近心跳</dt><dd>{dateTime(edge.runtime?.lastHeartbeat)}</dd></div></dl></section></div>
}

function EdgeDaemons({ edgeId }: { edgeId: string }) {
  const query = useProtoQuery(['daemons'], '/api/operator/daemons', ListDaemonsResponseSchema, 5_000)
  if (query.isPending) return <Skeleton />
  if (query.error) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const values = query.data?.daemons.filter((value) => value.runtime?.edgeId === edgeId) ?? []
  if (!values.length) return <Empty>当前没有 daemon 注册到这个 Edge</Empty>
  return <TableFrame><table><thead><tr><th>daemon</th><th>账号</th><th>设备</th><th>generation</th></tr></thead><tbody>{values.map((value) => <tr key={value.daemon?.daemonId}><td><strong>{value.daemon?.displayName}</strong><small className="mono">{compactID(value.daemon?.daemonId ?? '')}</small></td><td>{value.daemon?.accountName}<small className="mono">{compactID(value.daemon?.accountId ?? '')}</small></td><td>{value.daemon?.deviceId}</td><td>{value.runtime?.generation.toString()}</td></tr>)}</tbody></table></TableFrame>
}

function EdgeConnections({ edgeId }: { edgeId: string }) {
  const query = useProtoQuery(['connections'], '/api/operator/connections', ListRuntimeSessionsResponseSchema, 5_000)
  if (query.isPending) return <Skeleton />
  if (query.error) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const values = query.data?.sessions.filter((value) => value.edgeId === edgeId) ?? []
  if (!values.length) return <Empty>当前没有实时连接</Empty>
  return <TableFrame><table><thead><tr><th>Session</th><th>daemon</th><th>客户端</th><th>连接时间</th></tr></thead><tbody>{values.map((value) => <tr key={value.sessionId}><td className="mono">{compactID(value.sessionId)}</td><td className="mono">{compactID(value.daemonId)}</td><td className="mono">{compactID(value.clientId)}</td><td>{dateTime(value.connectedAt)}</td></tr>)}</tbody></table></TableFrame>
}

function EdgeCertificate({ edge }: { edge: ManagedEdge }) {
  const profiles = useProtoQuery(['certificates'], '/api/operator/certificates', ListCertificateProfilesResponseSchema, 30_000)
  const client = useQueryClient()
  const [selected, setSelected] = useState(edge.certificate?.certificateProfileId ?? '')
  const mutation = useMutation({ mutationFn: () => protoSend(
    `/api/operator/edges/${edge.config?.edgeId}/certificate`,
    BindCertificateProfileRequestSchema,
    create(BindCertificateProfileRequestSchema, { edgeId: edge.config?.edgeId, certificateProfileId: selected, expectedBindingRevision: edge.certificate?.bindingRevision ?? 0n }),
    BindCertificateProfileResponseSchema,
  ), onSuccess: () => {
    void client.invalidateQueries({ queryKey: ['edges'] })
    void client.invalidateQueries({ queryKey: ['certificates'] })
  } })
  if (profiles.isPending) return <Skeleton rows={3} />
  if (profiles.error) return <ErrorState error={profiles.error} onRetry={() => void profiles.refetch()} />
  return <section className="plain-section"><header><div><h2>公网证书</h2><p>{edge.config?.publicEndpoint}</p></div><KeyRound size={19} /></header><div className="certificate-detail">
    <dl className="detail-list"><div><dt>当前档案</dt><dd>{edge.certificate?.certificateProfileName || '未绑定'}</dd></div><div><dt>同步状态</dt><dd><CertificateState state={edge.certificate?.syncState} online={edge.runtime?.online} /></dd></div><div><dt>Desired / Applied</dt><dd>{edge.certificate ? `r${edge.certificate.desiredRevision} / r${edge.certificate.appliedRevision}` : '-'}</dd></div><div><dt>最近结果</dt><dd>{edge.certificate?.lastErrorMessage || dateTime(edge.certificate?.appliedAt)}</dd></div></dl>
    <div className="certificate-bind-control"><Field label="证书档案"><select className="input" value={selected} onChange={(event) => setSelected(event.target.value)}><option value="" disabled>请选择证书档案</option>{profiles.data?.profiles.map((profile) => <option key={profile.certificateProfileId} value={profile.certificateProfileId}>{profile.name} · r{profile.revision.toString()}</option>)}</select></Field><Button tone="primary" disabled={mutation.isPending || !selected || selected === (edge.certificate?.certificateProfileId ?? '')} onClick={() => mutation.mutate()}>保存绑定</Button></div>
    {mutation.error && <Notice tone="error">无法保存 Edge 证书绑定，请刷新后重试。{mutation.error instanceof APIError && mutation.error.correlationID ? ` 关联 ID：${mutation.error.correlationID}` : ''}</Notice>}
  </div></section>
}

function EdgeSettings({ edge }: { edge: ManagedEdge }) {
  const config = edge.config
  const client = useQueryClient()
  const [editing, setEditing] = useState(false)
  const initial = useMemo(() => ({ name: config?.name ?? '', region: config?.region ?? '', publicEndpoint: config?.publicEndpoint ?? '', capacity: config?.capacity.toString() ?? '0', enabled: config?.enabled ?? false }), [config])
  const [form, setForm] = useState(initial)
  const mutation = useMutation({ mutationFn: () => protoSend(`/api/operator/edges/${config?.edgeId}`, UpdateEdgeRequestSchema, create(UpdateEdgeRequestSchema, { edgeId: config?.edgeId, expectedRevision: edge.configRevision, name: form.name, region: form.region, publicEndpoint: form.publicEndpoint, capacity: BigInt(form.capacity), enabled: form.enabled }), UpdateEdgeResponseSchema, 'PUT'), onSuccess: () => { setEditing(false); void client.invalidateQueries({ queryKey: ['edges'] }) } })
  return <section className="plain-section"><header><div><h2>配置</h2><p>仅修改持久 desired state</p></div><Button onClick={() => { setForm(initial); setEditing(true) }}><Edit3 size={16} />编辑</Button></header><dl className="detail-list"><div><dt>名称</dt><dd>{config?.name}</dd></div><div><dt>区域</dt><dd>{config?.region}</dd></div><div><dt>入口</dt><dd className="mono">{config?.publicEndpoint}</dd></div><div><dt>状态</dt><dd>{config?.enabled ? '启用' : '停用'}</dd></div></dl><Dialog title="编辑 Edge 配置" open={editing} onClose={() => setEditing(false)} footer={<><Button tone="quiet" onClick={() => setEditing(false)}>取消</Button><Button tone="primary" onClick={() => mutation.mutate()} disabled={mutation.isPending}>保存</Button></>}><div className="form-grid"><Field label="名称"><Input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field><Field label="区域"><Input value={form.region} onChange={(event) => setForm({ ...form, region: event.target.value })} /></Field><Field label="域名或域名:端口"><Input value={form.publicEndpoint} onChange={(event) => setForm({ ...form, publicEndpoint: event.target.value })} /></Field><Field label="daemon 容量"><Input type="number" min="1" value={form.capacity} onChange={(event) => setForm({ ...form, capacity: event.target.value })} /></Field><label className="check-field"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} />启用此 Edge</label>{mutation.error && <p className="form-error" role="alert">无法保存 Edge 配置，请刷新后重试。{mutation.error instanceof APIError && mutation.error.correlationID ? ` 关联 ID：${mutation.error.correlationID}` : ''}</p>}</div></Dialog></section>
}
