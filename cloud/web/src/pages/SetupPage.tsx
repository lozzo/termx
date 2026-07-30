import { create } from '@bufbuild/protobuf'
import { useMutation } from '@tanstack/react-query'
import { ArrowRight, Eye, EyeOff } from 'lucide-react'
import { FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { APIError, protoSend } from '../api'
import { RedeemAccountSetupRequestSchema, RedeemAccountSetupResponseSchema } from '../generated/cloud/v1/account_pb'
import { isValidPassword } from '../password'
import { AuthLayout } from '../shell/AuthLayout'
import { Button, Field, Input } from '../ui'

const setupFragmentPattern = /^#[A-Za-z0-9_-]{43}$/

export function SetupPage() {
	const [credential, setCredential] = useState(() => setupFragmentPattern.test(window.location.hash) ? window.location.hash.slice(1) : '')
	const [password, setPassword] = useState('')
	const [confirmation, setConfirmation] = useState('')
	const [showPassword, setShowPassword] = useState(false)
	const [localError, setLocalError] = useState('')
	const navigate = useNavigate()
	const mutation = useMutation({
		mutationFn: () => protoSend('/api/account/setup/redeem', RedeemAccountSetupRequestSchema, create(RedeemAccountSetupRequestSchema, { setupCredential: credential, newPassword: password }), RedeemAccountSetupResponseSchema),
		onSuccess: () => { setPassword(''); setConfirmation(''); setCredential(''); navigate('/app/overview', { replace: true }) },
	})
	useEffect(() => {
		document.title = '设置账号密码 · AnyTTY Cloud'
		if (window.location.hash) window.history.replaceState(window.history.state, '', `${window.location.pathname}${window.location.search}`)
	}, [])
	function submit(event: FormEvent) {
		event.preventDefault()
		setLocalError('')
		if (!isValidPassword(password)) { setLocalError('密码须为 8–72 个 UTF-8 字节。'); return }
		if (password !== confirmation) { setLocalError('两次输入的密码不一致。'); return }
		mutation.mutate()
	}
	const correlationID = mutation.error instanceof APIError ? mutation.error.correlationID : ''
	const remoteError = mutation.error instanceof APIError && mutation.error.status === 429
		? '尝试过于频繁，请稍后重试。'
		: '一次性凭据无效或已过期，请向管理员申请重置。'
	return <AuthLayout title="设置账号密码" description="输入管理员提供的一次性凭据，为 Cloud 账号设置密码。" alternate={<Link to="/login">返回登录</Link>}>
		<form onSubmit={submit}>
			<Field label="一次性凭据" hint="该凭据只能使用一次"><Input autoComplete="off" autoFocus spellCheck={false} value={credential} onChange={(event) => setCredential(event.target.value)} /></Field>
			<Field label="新密码" hint="8–72 个 UTF-8 字节" htmlFor="setup-password"><div className="password-input"><Input id="setup-password" type={showPassword ? 'text' : 'password'} autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} /><button type="button" aria-label={showPassword ? '隐藏密码' : '显示密码'} onClick={() => setShowPassword((value) => !value)}>{showPassword ? <EyeOff size={18} /> : <Eye size={18} />}</button></div></Field>
			<Field label="确认新密码"><Input type={showPassword ? 'text' : 'password'} autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></Field>
			{localError && <p className="form-error" role="alert">{localError}</p>}
			{mutation.error && <p className="form-error" role="alert">{remoteError}{correlationID ? ` 关联 ID：${correlationID}` : ''}</p>}
			<Button tone="primary" type="submit" disabled={credential.length !== 43 || !isValidPassword(password) || !isValidPassword(confirmation) || mutation.isPending}>{mutation.isPending ? '正在设置' : '设置密码'}<ArrowRight size={17} /></Button>
		</form>
	</AuthLayout>
}
