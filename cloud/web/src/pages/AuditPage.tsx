import { Search } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { compactID, dateTime } from '../format'
import { ListOperatorAuditResponseSchema } from '../generated/cloud/v1/operator_pb'
import { CursorPagination, pageURL, useCursorPagination } from '../pagination'
import { useProtoQuery } from '../query'
import { Button, Empty, ErrorState, PageHeader, Skeleton, Status, TableFrame } from '../ui'

export function AuditPage() {
  const [search, setSearch] = useState('')
  const [queryValue, setQueryValue] = useState('')
  const pagination = useCursorPagination()
  const query = useProtoQuery(['audit', { query: queryValue, cursor: pagination.cursor }], pageURL('/api/operator/audit', pagination.cursor, queryValue), ListOperatorAuditResponseSchema, 10_000)
  function submit(event: FormEvent) { event.preventDefault(); pagination.reset(); setQueryValue(search.trim()) }
  return <>
    <PageHeader title="审计" meta="持久记录管理 mutation 与实时控制命令结果" />
    <form className="list-toolbar" onSubmit={submit}><div className="search-input"><Search size={16} aria-hidden="true" /><label className="visually-hidden" htmlFor="audit-search">搜索审计记录</label><input id="audit-search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="操作、对象 ID 或操作人" /></div><Button type="submit">查询</Button></form>
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : !query.data?.events.length ? <Empty>暂无审计记录</Empty> : <><TableFrame><table><thead><tr><th>时间</th><th>操作人</th><th>操作</th><th>对象</th><th>原因</th><th>结果</th><th>关联 ID</th></tr></thead><tbody>{query.data.events.map((value) => <tr key={value.auditId}><td>{dateTime(value.occurredAt)}</td><td>{value.actorDisplayName}<small className="mono">{compactID(value.actorAccountId)}</small></td><td className="mono">{value.action}</td><td>{value.resourceType}<small className="mono">{compactID(value.resourceId)}</small></td><td>{value.reason || '-'}</td><td><Status active={value.result === 'applied'}>{value.result}</Status></td><td className="mono">{compactID(value.correlationId)}</td></tr>)}</tbody></table></TableFrame><CursorPagination page={pagination.page} nextCursor={query.data.nextCursor} onPrevious={pagination.previous} onNext={pagination.next} /></>}
  </>
}
