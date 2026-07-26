import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCcw } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { protoSend } from '../api'
import { compactID, dateTime, subscriptionState } from '../format'
import { SubscriptionState, SubscriptionTransition, TransitionSubscriptionRequestSchema, TransitionSubscriptionResponseSchema, type SubscriptionProjection } from '../generated/cloud/v1/commerce_pb'
import { ListOperatorSubscriptionsResponseSchema } from '../generated/cloud/v1/operator_pb'
import { CursorPagination, pageURL, useCursorPagination } from '../pagination'
import { useProtoQuery } from '../query'
import { Button, Dialog, Empty, ErrorState, Field, Input, Notice, PageHeader, Skeleton, Status, TableFrame } from '../ui'

const transitions = [
  [SubscriptionTransition.ACTIVATE, '激活'], [SubscriptionTransition.RENEW, '续期'], [SubscriptionTransition.UPGRADE, '升级'], [SubscriptionTransition.DOWNGRADE, '降级'], [SubscriptionTransition.CANCEL_AT_PERIOD_END, '到期取消'], [SubscriptionTransition.RESUME, '恢复续订'], [SubscriptionTransition.SUSPEND, '暂停'], [SubscriptionTransition.RESTORE, '恢复'], [SubscriptionTransition.EXPIRE, '标记到期'], [SubscriptionTransition.REVOKE, '撤销'],
] as const

export function SubscriptionsPage() {
  const pagination = useCursorPagination()
  const query = useProtoQuery(['subscriptions', { cursor: pagination.cursor }], pageURL('/api/operator/subscriptions', pagination.cursor), ListOperatorSubscriptionsResponseSchema, 15_000)
  const client = useQueryClient()
  const [target, setTarget] = useState<SubscriptionProjection>()
  const [transition, setTransition] = useState(SubscriptionTransition.SUSPEND)
  const [planID, setPlanID] = useState('')
  const [planVersion, setPlanVersion] = useState('0')
  const [reason, setReason] = useState('')
  const mutation = useMutation({ mutationFn: () => protoSend('/api/operator/subscriptions/transition', TransitionSubscriptionRequestSchema, create(TransitionSubscriptionRequestSchema, { accountId: target?.accountId, transition, targetPlanId: planID, targetPlanVersion: BigInt(planVersion || 0), expectedRevision: target?.revision, reason }), TransitionSubscriptionResponseSchema), onSuccess: () => { setTarget(undefined); setReason(''); void client.invalidateQueries({ queryKey: ['subscriptions'] }); void client.invalidateQueries({ queryKey: ['accounts'] }) } })
  return <>
    <PageHeader title="订阅" meta="订阅状态变更与生效 Entitlement 在同一持久事务中收敛" />
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} /> : !query.data?.subscriptions.length ? <Empty>暂无订阅</Empty> : <><TableFrame><table><thead><tr><th>订阅</th><th>账号</th><th>套餐版本</th><th>状态</th><th>周期</th><th>修订号</th><th /></tr></thead><tbody>{query.data.subscriptions.map((value) => <tr key={value.subscriptionId}><td className="mono">{compactID(value.subscriptionId)}</td><td><Link className="table-link mono" to={`/accounts/${value.accountId}`}>{compactID(value.accountId)}</Link></td><td>{value.planId} v{value.planVersion.toString()}</td><td><Status active={value.state === SubscriptionState.ACTIVE}>{subscriptionState(value.state)}</Status></td><td>{dateTime(value.periodStart)}<small>至 {dateTime(value.periodEnd)}</small></td><td>{value.revision.toString()}</td><td><Button onClick={() => { setTarget(value); setPlanID(value.planId); setPlanVersion(value.planVersion.toString()) }}><RefreshCcw size={15} />变更</Button></td></tr>)}</tbody></table></TableFrame><CursorPagination page={pagination.page} nextCursor={query.data.nextCursor} onPrevious={pagination.previous} onNext={pagination.next} /></>}
    <Dialog title="变更订阅" open={Boolean(target)} onClose={() => setTarget(undefined)} footer={<><Button tone="quiet" onClick={() => setTarget(undefined)}>取消</Button><Button tone="primary" onClick={() => mutation.mutate()} disabled={!reason.trim() || mutation.isPending}>确认变更</Button></>}><Notice tone="warning">账号 {compactID(target?.accountId ?? '')} 的 Cloud 准入限制会按新状态重新计算。</Notice><div className="form-grid"><Field label="操作"><select className="input" value={transition} onChange={(event) => setTransition(Number(event.target.value))}>{transitions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></Field><Field label="目标套餐 ID"><Input value={planID} onChange={(event) => setPlanID(event.target.value)} /></Field><Field label="目标版本"><Input type="number" min="0" value={planVersion} onChange={(event) => setPlanVersion(event.target.value)} /></Field><Field label="操作原因"><Input value={reason} onChange={(event) => setReason(event.target.value)} /></Field>{mutation.error && <p className="form-error">{mutation.error.message}</p>}</div></Dialog>
  </>
}
