import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Search, Shield, UserRoundCog } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { APIError, protoSend } from '../api'
import { accountState, bytes, compactID, dateTime, entitlementState, money, orderState, role, subscriptionState } from '../format'
import { AccountRole, AccountState } from '../generated/cloud/v1/account_pb'
import { GetAccountCommerceResponseSchema } from '../generated/cloud/v1/commerce_pb'
import { GetOperatorAccountResponseSchema, ListOperatorAccountsResponseSchema, SetAccountRoleRequestSchema, SetAccountRoleResponseSchema, SetAccountStateRequestSchema, SetAccountStateResponseSchema, type AccountSummary } from '../generated/cloud/v1/operator_pb'
import { CursorPagination, pageURL, useCursorPagination } from '../pagination'
import { useProtoQuery } from '../query'
import { Button, Dialog, Empty, ErrorState, Field, Input, Notice, PageHeader, Skeleton, Status, TableFrame } from '../ui'

export function AccountsPage() {
  const [params, setParams] = useSearchParams()
  const [search, setSearch] = useState(params.get('query') ?? '')
  const queryValue = params.get('query') ?? ''
  const pagination = useCursorPagination()
  const query = useProtoQuery(['accounts', { query: queryValue, cursor: pagination.cursor }], pageURL('/api/operator/accounts', pagination.cursor, queryValue), ListOperatorAccountsResponseSchema, 15_000)
  function submit(event: FormEvent) { event.preventDefault(); pagination.reset(); setParams(search.trim() ? { query: search.trim() } : {}) }
  return <>
    <PageHeader title="用户与权限" meta="账号、角色、订阅、Entitlement 与当期用量" />
    <form className="list-toolbar" onSubmit={submit}><div className="search-input"><Search size={16} aria-hidden="true" /><label className="visually-hidden" htmlFor="account-search">搜索账号</label><input id="account-search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="账号名称、邮箱或 ID" /></div><Button type="submit">查询</Button></form>
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : !query.data?.accounts.length ? <Empty>没有匹配的账号</Empty> : <><AccountTable values={query.data.accounts} /><CursorPagination page={pagination.page} nextCursor={query.data.nextCursor} onPrevious={pagination.previous} onNext={pagination.next} /></>}
  </>
}

function AccountTable({ values }: { values: AccountSummary[] }) {
  return <TableFrame><table><thead><tr><th>账号</th><th>角色</th><th>状态</th><th>daemon</th><th>订阅</th><th>当期 Relay</th><th /></tr></thead><tbody>{values.map((value) => <tr key={value.account?.accountId}><td><strong>{value.account?.displayName}</strong><small>{value.account?.email || compactID(value.account?.accountId ?? '')}</small></td><td>{value.roles.map((current) => role(current)).join('、')}</td><td><Status active={value.account?.state === AccountState.ACTIVE}>{accountState(value.account?.state ?? AccountState.UNSPECIFIED)}</Status></td><td>{value.daemonCount.toString()}</td><td>{value.subscription ? <><strong>{value.subscription.planId}</strong><small>{subscriptionState(value.subscription.state)}</small></> : '-'}</td><td>{bytes(value.usage?.relayTotalBytes ?? 0n)}<small>剩余 {bytes(value.usage?.remainingBytes ?? 0n)}</small></td><td><Link className="table-link" to={`/app/admin/accounts/${value.account?.accountId}`}>详情</Link></td></tr>)}</tbody></table></TableFrame>
}

export function AccountDetailPage() {
  const { accountId = '' } = useParams()
  const query = useProtoQuery(['accounts', accountId], `/api/operator/accounts/${accountId}`, GetOperatorAccountResponseSchema, 10_000)
  const commerce = useProtoQuery(['commerce', accountId], `/api/commerce/account/${accountId}`, GetAccountCommerceResponseSchema, 15_000)
  const client = useQueryClient()
  const [stateOpen, setStateOpen] = useState(false)
  const [roleOpen, setRoleOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [selectedRole, setSelectedRole] = useState(AccountRole.OPERATOR)
  const [enableRole, setEnableRole] = useState(true)
  const summary = query.data?.account
  const profile = summary?.account
  const setState = useMutation({ mutationFn: () => protoSend(`/api/operator/accounts/${accountId}/state`, SetAccountStateRequestSchema, create(SetAccountStateRequestSchema, { accountId, expectedRevision: profile?.revision, state: profile?.state === AccountState.ACTIVE ? AccountState.DISABLED : AccountState.ACTIVE, reason }), SetAccountStateResponseSchema), onSuccess: () => { setStateOpen(false); setReason(''); void client.invalidateQueries({ queryKey: ['accounts'] }) } })
  const setRole = useMutation({ mutationFn: () => protoSend(`/api/operator/accounts/${accountId}/role`, SetAccountRoleRequestSchema, create(SetAccountRoleRequestSchema, { accountId, role: selectedRole, enabled: enableRole, reason }), SetAccountRoleResponseSchema), onSuccess: () => { setRoleOpen(false); setReason(''); void client.invalidateQueries({ queryKey: ['accounts'] }) } })
  if (query.isPending) return <Skeleton />
  if (query.error || !summary || !profile) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  return <>
    <PageHeader title={profile.displayName} meta={`${profile.email || '未填写邮箱'} · ${profile.accountId}`} actions={<><Button onClick={() => setRoleOpen(true)}><Shield size={16} />调整角色</Button><Button tone={profile.state === AccountState.ACTIVE ? 'danger' : 'primary'} onClick={() => setStateOpen(true)}><UserRoundCog size={16} />{profile.state === AccountState.ACTIVE ? '禁用账号' : '恢复账号'}</Button></>} />
    <div className="detail-columns"><section className="plain-section"><header><h2>账号资料</h2></header><dl className="detail-list"><div><dt>状态</dt><dd><Status active={profile.state === AccountState.ACTIVE}>{accountState(profile.state)}</Status></dd></div><div><dt>角色</dt><dd>{summary.roles.map((current) => role(current)).join('、')}</dd></div><div><dt>daemon 数</dt><dd>{summary.daemonCount.toString()}</dd></div><div><dt>创建时间</dt><dd>{dateTime(profile.createdAt)}</dd></div><div><dt>修订号</dt><dd>{profile.revision.toString()}</dd></div></dl></section><section className="plain-section"><header><h2>当前 Entitlement</h2></header>{summary.entitlement ? <dl className="detail-list"><div><dt>状态</dt><dd>{entitlementState(summary.entitlement.state)}</dd></div><div><dt>套餐</dt><dd>{summary.entitlement.planId} v{summary.entitlement.planVersion.toString()}</dd></div><div><dt>托管 P2P</dt><dd>{summary.entitlement.capability?.managedP2pEnabled ? '可用' : '不可用'}</dd></div><div><dt>Relay</dt><dd>{summary.entitlement.capability?.relayEnabled ? '可用' : '不可用'}</dd></div><div><dt>剩余流量</dt><dd>{bytes(summary.entitlement.relayRemainingBytes)}</dd></div></dl> : <Empty>无生效权益</Empty>}</section></div>
    <section className="plain-section"><header><h2>订单记录</h2></header>{commerce.isPending ? <Skeleton rows={3} /> : commerce.error ? <ErrorState error={commerce.error} onRetry={() => void commerce.refetch()} /> : !commerce.data?.orders.length ? <Empty>暂无订单</Empty> : <TableFrame><table><thead><tr><th>订单</th><th>套餐</th><th>状态</th><th>金额</th><th>创建时间</th></tr></thead><tbody>{commerce.data.orders.map((order) => <tr key={order.orderId}><td className="mono">{compactID(order.orderId)}</td><td>{order.planId} v{order.planVersion.toString()}</td><td>{orderState(order.status)}</td><td>{money(order.amount?.currency ?? 'CNY', order.amount?.minorUnits ?? 0n)}</td><td>{dateTime(order.createdAt)}</td></tr>)}</tbody></table></TableFrame>}</section>
    <Dialog title={profile.state === AccountState.ACTIVE ? '禁用账号' : '恢复账号'} open={stateOpen} onClose={() => setStateOpen(false)} footer={<><Button tone="quiet" onClick={() => setStateOpen(false)}>取消</Button><Button tone={profile.state === AccountState.ACTIVE ? 'danger' : 'primary'} onClick={() => setState.mutate()} disabled={!reason.trim() || setState.isPending}>确认</Button></>}><Notice tone="warning">账号状态变更会影响后续 Cloud 准入，不会伪造或保留旧在线拓扑。</Notice><Field label="操作原因"><Input autoFocus value={reason} onChange={(event) => setReason(event.target.value)} /></Field>{setState.error && <p className="form-error" role="alert">无法变更账号状态，请刷新后重试。{setState.error instanceof APIError && setState.error.correlationID ? ` 关联 ID：${setState.error.correlationID}` : ''}</p>}</Dialog>
    <Dialog title="调整运营角色" open={roleOpen} onClose={() => setRoleOpen(false)} footer={<><Button tone="quiet" onClick={() => setRoleOpen(false)}>取消</Button><Button tone="primary" onClick={() => setRole.mutate()} disabled={!reason.trim() || setRole.isPending}>保存</Button></>}><div className="form-grid"><Field label="角色"><select className="input" value={selectedRole} onChange={(event) => setSelectedRole(Number(event.target.value))}><option value={AccountRole.OPERATOR}>运营</option><option value={AccountRole.ADMIN}>管理员</option></select></Field><label className="check-field"><input type="checkbox" checked={enableRole} onChange={(event) => setEnableRole(event.target.checked)} />授予角色</label><Field label="操作原因"><Input value={reason} onChange={(event) => setReason(event.target.value)} /></Field>{setRole.error && <p className="form-error" role="alert">无法调整运营角色，请刷新后重试。{setRole.error instanceof APIError && setRole.error.correlationID ? ` 关联 ID：${setRole.error.correlationID}` : ''}</p>}</div></Dialog>
  </>
}
