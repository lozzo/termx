import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Eye, EyeOff } from 'lucide-react'
import { FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { protoSend } from '../api'
import { RegisterAccountRequestSchema, RegisterAccountResponseSchema } from '../generated/cloud/v1/account_pb'
import { AuthLayout } from '../shell/AuthLayout'
import { Button, Field, Input } from '../ui'

export function RegisterPage() {
  const [displayName, setDisplayName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => protoSend('/api/account/register', RegisterAccountRequestSchema, create(RegisterAccountRequestSchema, { displayName, email, password }), RegisterAccountResponseSchema),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['account', 'current'] }); navigate('/app/overview', { replace: true }) },
  })
  useEffect(() => { document.title = '创建账号 · AnyTTY Cloud' }, [])
  function submit(event: FormEvent) { event.preventDefault(); if (displayName.trim() && email.includes('@') && password.length >= 8) mutation.mutate() }
  return <AuthLayout title="创建 AnyTTY 账号" description="免费添加第一台设备，使用托管 P2P 与 Relay 从外网安全连接。" alternate={<>已经有账号？<Link to="/login">直接登录</Link></>}>
    <form onSubmit={submit}>
      <Field label="你的称呼"><Input autoComplete="name" autoFocus placeholder="例如：小明" value={displayName} onChange={(event) => setDisplayName(event.target.value)} /></Field>
      <Field label="邮箱"><Input type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} /></Field>
      <Field label="密码" hint="至少 8 个字符" htmlFor="register-password"><div className="password-input"><Input id="register-password" type={showPassword ? 'text' : 'password'} autoComplete="new-password" minLength={8} value={password} onChange={(event) => setPassword(event.target.value)} /><button type="button" aria-label={showPassword ? '隐藏密码' : '显示密码'} onClick={() => setShowPassword((value) => !value)}>{showPassword ? <EyeOff size={18} /> : <Eye size={18} />}</button></div></Field>
      {mutation.error && <p className="form-error" role="alert">无法创建账号。该邮箱可能已经注册，请检查后重试。</p>}
      <Button tone="primary" type="submit" disabled={!displayName.trim() || !email.includes('@') || password.length < 8 || mutation.isPending}>{mutation.isPending ? '正在创建' : '创建账号'}<ArrowRight size={17} /></Button>
    </form>
  </AuthLayout>
}
