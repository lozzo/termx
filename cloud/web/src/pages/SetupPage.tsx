import { create } from '@bufbuild/protobuf'
import { useMutation } from '@tanstack/react-query'
import { ArrowRight, Eye, EyeOff } from 'lucide-react'
import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router'
import { APIError, protoSend } from '../api'
import { RedeemAccountSetupRequestSchema, RedeemAccountSetupResponseSchema } from '../generated/cloud/v1/account_pb'
import { AuthLayout } from '../shell/AuthLayout'
import { Button, Field, Input, Notice } from '../ui'

export function SetupPage() {
	const [credential, setCredential] = useState('')
	const [password, setPassword] = useState('')
	const [confirmation, setConfirmation] = useState('')
	const [showPassword, setShowPassword] = useState(false)
	const [localError, setLocalError] = useState('')
	const mutation = useMutation({
		mutationFn: () => protoSend('/api/account/setup/redeem', RedeemAccountSetupRequestSchema, create(RedeemAccountSetupRequestSchema, { setupCredential: credential.trim(), newPassword: password }), RedeemAccountSetupResponseSchema),
		onSuccess: () => { setPassword(''); setConfirmation(''); setCredential('') },
	})
	useEffect(() => { document.title = '设置账号密码 · AnyTTY Cloud' }, [])
	function submit(event: FormEvent) {
		event.preventDefault()
		setLocalError('')
		if (password !== confirmation) { setLocalError('两次输入的密码不一致。'); return }
		mutation.mutate()
	}
	const correlationID = mutation.error instanceof APIError ? mutation.error.correlationID : ''
	const remoteError = mutation.error instanceof APIError && mutation.error.status === 429
		? '尝试过于频繁，请稍后重试。'
		: '一次性凭据无效或已过期，请向管理员申请重置。'
	return <AuthLayout title="设置账号密码" description="输入管理员提供的一次性凭据，为 Cloud 账号设置密码。" alternate={<Link to="/login">返回登录</Link>}>
		{mutation.isSuccess ? <div className="setup-success"><Notice>密码已设置，账号现在可以登录。</Notice><Link className="button button-primary" to="/login">前往登录<ArrowRight size={17} /></Link></div> : <form onSubmit={submit}>
			<Field label="一次性凭据" hint="该凭据只能使用一次"><Input autoComplete="off" autoFocus spellCheck={false} value={credential} onChange={(event) => setCredential(event.target.value)} /></Field>
			<Field label="新密码" htmlFor="setup-password"><div className="password-input"><Input id="setup-password" type={showPassword ? 'text' : 'password'} autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} /><button type="button" aria-label={showPassword ? '隐藏密码' : '显示密码'} onClick={() => setShowPassword((value) => !value)}>{showPassword ? <EyeOff size={18} /> : <Eye size={18} />}</button></div></Field>
			<Field label="确认新密码"><Input type={showPassword ? 'text' : 'password'} autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></Field>
			{localError && <p className="form-error" role="alert">{localError}</p>}
			{mutation.error && <p className="form-error" role="alert">{remoteError}{correlationID ? ` 关联 ID：${correlationID}` : ''}</p>}
			<Button tone="primary" type="submit" disabled={!credential.trim() || password.length < 8 || confirmation.length < 8 || mutation.isPending}>{mutation.isPending ? '正在设置' : '设置密码'}<ArrowRight size={17} /></Button>
		</form>}
	</AuthLayout>
}
