import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Eye, EyeOff } from 'lucide-react'
import { FormEvent, useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router'
import { APIError, protoSend } from '../api'
import { LoginAccountRequestSchema, LoginAccountResponseSchema } from '../generated/cloud/v1/account_pb'
import { isValidPassword } from '../password'
import { Button, Field, Input } from '../ui'
import { AuthLayout } from '../shell/AuthLayout'

function loginErrorMessage(error: unknown): string {
  if (error instanceof APIError) {
    if (error.status === 429) return '登录尝试过于频繁。请稍后再试。'
    if (error.status >= 500 && error.status <= 599) return '登录服务暂时不可用。请稍后重试。'
    return '账号或密码不正确。请检查后重新输入。'
  }
  return '暂时无法连接登录服务。请检查网络后重试。'
}

export function LoginPage() {
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => protoSend('/api/account/login', LoginAccountRequestSchema, create(LoginAccountRequestSchema, { login, password }), LoginAccountResponseSchema),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['account', 'current'] }); const from = (location.state as { from?: string } | null)?.from; navigate(from?.startsWith('/app/') ? from : '/app/overview', { replace: true }) },
  })
  useEffect(() => { document.title = '登录 · AnyTTY Cloud' }, [])
  function submit(event: FormEvent) { event.preventDefault(); if (login.trim() && isValidPassword(password)) mutation.mutate() }
  return <AuthLayout title="欢迎回来" description="登录 Cloud 控制台管理 daemon、路由、Relay、用量和订阅；AnyTTY App 无需此账号。" alternate={<>账号由部署管理员提供</>}>
    <form onSubmit={submit}>
      <Field label="邮箱或账号"><Input autoComplete="username" autoFocus value={login} onChange={(event) => setLogin(event.target.value)} /></Field>
      <Field label="密码" hint="8–72 个 UTF-8 字节" htmlFor="login-password"><div className="password-input"><Input id="login-password" type={showPassword ? 'text' : 'password'} autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} /><button type="button" aria-label={showPassword ? '隐藏密码' : '显示密码'} onClick={() => setShowPassword((value) => !value)}>{showPassword ? <EyeOff size={18} /> : <Eye size={18} />}</button></div></Field>
      {mutation.error && <p className="form-error" role="alert">{loginErrorMessage(mutation.error)}</p>}
      <Button tone="primary" type="submit" disabled={!login.trim() || !isValidPassword(password) || mutation.isPending}>{mutation.isPending ? '正在登录' : '登录'}<ArrowRight size={17} /></Button>
    </form>
  </AuthLayout>
}
