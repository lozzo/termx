import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Laptop, ShieldCheck, ShieldX } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { protoSend } from '../api'
import { dateTime } from '../format'
import { ChangeAccountPasswordRequestSchema, ChangeAccountPasswordResponseSchema, ListAccountSessionsResponseSchema, RevokeAccountSessionRequestSchema, RevokeAccountSessionResponseSchema, VerifyRecentAuthenticationRequestSchema, VerifyRecentAuthenticationResponseSchema } from '../generated/cloud/v1/account_pb'
import { useProtoQuery } from '../query'
import { useCloudAccount } from '../shell/CloudShell'
import { Button, Dialog, ErrorState, Field, Input, Notice, PageHeader, Skeleton, Status, TableFrame } from '../ui'

export function SecurityPage() {
  const { current } = useCloudAccount()
  const sessions = useProtoQuery(['account', 'sessions'], '/api/account/sessions', ListAccountSessionsResponseSchema, 15_000)
  const client = useQueryClient()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [targetSession, setTargetSession] = useState('')
  const [reauthOpen, setReauthOpen] = useState(false)
  const [reauthPassword, setReauthPassword] = useState('')
  const change = useMutation({ mutationFn: () => protoSend('/api/account/password', ChangeAccountPasswordRequestSchema, create(ChangeAccountPasswordRequestSchema, { currentPassword, newPassword }), ChangeAccountPasswordResponseSchema), onSuccess: () => { setCurrentPassword(''); setNewPassword(''); setConfirmPassword(''); void client.invalidateQueries({ queryKey: ['account'] }) } })
  const revoke = useMutation({ mutationFn: () => protoSend('/api/account/sessions', RevokeAccountSessionRequestSchema, create(RevokeAccountSessionRequestSchema, { sessionId: targetSession }), RevokeAccountSessionResponseSchema, 'DELETE'), onSuccess: () => { setTargetSession(''); void client.invalidateQueries({ queryKey: ['account', 'sessions'] }) } })
  const verify = useMutation({ mutationFn: () => protoSend('/api/account/recent-auth', VerifyRecentAuthenticationRequestSchema, create(VerifyRecentAuthenticationRequestSchema, { password: reauthPassword }), VerifyRecentAuthenticationResponseSchema), onSuccess: () => { setReauthOpen(false); setReauthPassword(''); void client.invalidateQueries({ queryKey: ['account', 'current'] }) } })
  function submitPassword(event: FormEvent) { event.preventDefault(); if (newPassword.length >= 8 && newPassword === confirmPassword) change.mutate() }
  return <>
    <PageHeader title="账号安全" meta="管理密码和当前账号的登录会话" actions={<Button onClick={() => setReauthOpen(true)}><ShieldCheck size={16} />重新验证身份</Button>} />
    <div className="security-grid">
      <section className="plain-section account-profile"><header><div><h2>账号</h2><p>账号资料由 Controller 持久保存</p></div><Status active>正常</Status></header><dl className="detail-list"><div><dt>显示名称</dt><dd>{current.account?.displayName}</dd></div><div><dt>邮箱</dt><dd>{current.account?.email}</dd></div><div><dt>账号 ID</dt><dd className="mono">{current.account?.accountId}</dd></div><div><dt>最近认证有效至</dt><dd>{dateTime(current.recentAuthExpiresAt)}</dd></div></dl></section>
      <section className="plain-section"><header><div><h2>修改密码</h2><p>修改后其它登录会话会立即撤销</p></div><KeyRound size={20} /></header><form className="form-grid" onSubmit={submitPassword}><Field label="当前密码"><Input type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} /></Field><Field label="新密码" hint="至少 8 个字符"><Input type="password" autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></Field><Field label="确认新密码"><Input type="password" autoComplete="new-password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} /></Field>{confirmPassword && newPassword !== confirmPassword && <p className="form-error" role="alert">两次输入的新密码不一致。</p>}{change.error && <p className="form-error" role="alert">当前密码不正确，或新密码不符合要求。</p>}{change.isSuccess && <Notice>密码已更新，其它会话已撤销。</Notice>}<Button tone="primary" type="submit" disabled={!currentPassword || newPassword.length < 8 || newPassword !== confirmPassword || change.isPending}>更新密码</Button></form></section>
    </div>
    <section className="plain-section sessions-section"><header><div><h2>登录会话</h2><p>只显示仍在 refresh 有效期内的会话</p></div><Laptop size={20} /></header>{sessions.isPending ? <Skeleton rows={3} /> : sessions.error ? <ErrorState error={sessions.error} /> : <div className="user-data-table"><TableFrame><table><thead><tr><th>会话</th><th>创建时间</th><th>访问有效至</th><th>刷新有效至</th><th /></tr></thead><tbody>{sessions.data?.sessions.map((value) => <tr key={value.sessionId}><td data-label="会话"><span className="mono">{value.sessionId}</span>{value.current && <small>当前会话</small>}</td><td data-label="创建时间">{dateTime(value.createdAt)}</td><td data-label="访问有效至">{dateTime(value.accessExpiresAt)}</td><td data-label="刷新有效至">{dateTime(value.refreshExpiresAt)}</td><td data-label="">{!value.current && <Button tone="danger" onClick={() => setTargetSession(value.sessionId)}><ShieldX size={15} />撤销</Button>}</td></tr>)}</tbody></table></TableFrame></div>}</section>
    <Dialog title="撤销登录会话" open={Boolean(targetSession)} onClose={() => setTargetSession('')} footer={<><Button tone="quiet" onClick={() => setTargetSession('')}>取消</Button><Button tone="danger" onClick={() => revoke.mutate()} disabled={revoke.isPending}>确认撤销</Button></>}><Notice tone="warning">该会话将无法继续刷新或访问你的 Cloud 数据。</Notice>{revoke.error && <p className="form-error" role="alert">需要先重新验证身份，或该会话已失效。</p>}</Dialog>
    <Dialog title="重新验证身份" open={reauthOpen} onClose={() => setReauthOpen(false)} footer={<><Button tone="quiet" onClick={() => setReauthOpen(false)}>取消</Button><Button tone="primary" onClick={() => verify.mutate()} disabled={!reauthPassword || verify.isPending}>确认</Button></>}><Field label="当前密码"><Input type="password" autoFocus autoComplete="current-password" value={reauthPassword} onChange={(event) => setReauthPassword(event.target.value)} /></Field>{verify.error && <p className="form-error" role="alert">密码不正确。</p>}</Dialog>
  </>
}
