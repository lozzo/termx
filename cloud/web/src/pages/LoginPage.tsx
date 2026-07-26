import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, LockKeyhole } from 'lucide-react'
import { FormEvent, useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { protoSend } from '../api'
import { LoginAccountRequestSchema, LoginAccountResponseSchema } from '../generated/cloud/v1/account_pb'
import { Button, Field, Input } from '../ui'

export function LoginPage() {
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => protoSend('/api/account/login', LoginAccountRequestSchema, create(LoginAccountRequestSchema, { login, password }), LoginAccountResponseSchema),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['account', 'current'] }); navigate('/overview', { replace: true }) },
  })
  useEffect(() => { document.title = '登录 · Muxvia Cloud' }, [])
  function submit(event: FormEvent) { event.preventDefault(); if (login.trim() && password) mutation.mutate() }
  return <main className="login-page">
    <section className="login-panel">
      <div className="login-brand"><div className="brand-mark">M</div><div><strong>Muxvia Cloud</strong><span>运营管理后台</span></div></div>
      <div className="login-heading"><span className="login-kicker"><LockKeyhole size={16} />受控访问</span><h1>运营人员登录</h1><p>使用管理员或运营账号进入当前环境。</p></div>
      <form onSubmit={submit}>
        <Field label="账号"><Input autoComplete="username" autoFocus value={login} onChange={(event) => setLogin(event.target.value)} /></Field>
        <Field label="密码"><Input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} /></Field>
        {mutation.error && <p className="form-error" role="alert">{mutation.error.message}</p>}
        <Button tone="primary" type="submit" disabled={!login.trim() || !password || mutation.isPending}>登录<ArrowRight size={17} /></Button>
      </form>
      <footer><span>Development</span><span>muxvia.com</span></footer>
    </section>
    <aside className="login-context" aria-hidden="true"><div className="context-grid"><i /><i /><i /><i /></div><div className="route-signal"><span>CN1</span><b /><span>Controller</span><b /><span>Edge</span></div></aside>
  </main>
}
