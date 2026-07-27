import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { FileKey2, Link2, RefreshCw, Upload } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { protoSend } from '../api'
import { compactID, dateTime } from '../format'
import {
  BindCertificateProfileRequestSchema,
  BindCertificateProfileResponseSchema,
  CertificateSyncState,
  ListCertificateProfilesResponseSchema,
  UploadCertificateProfileRequestSchema,
  UploadCertificateProfileResponseSchema,
  type CertificateBinding,
  type CertificateProfile,
} from '../generated/cloud/v1/certificate_pb'
import { ListEdgesResponseSchema } from '../generated/cloud/v1/edge_config_pb'
import { useProtoQuery } from '../query'
import { Button, Dialog, Empty, ErrorState, Field, Input, Notice, PageHeader, Skeleton, TableFrame } from '../ui'

export function CertificatesPage() {
  const profiles = useProtoQuery(['certificates'], '/api/operator/certificates', ListCertificateProfilesResponseSchema, 30_000)
  const edges = useProtoQuery(['edges'], '/api/operator/edges', ListEdgesResponseSchema, 30_000)
  const client = useQueryClient()
  const [editing, setEditing] = useState<CertificateProfile | null | undefined>(undefined)
  const [name, setName] = useState('')
  const [certificateFile, setCertificateFile] = useState<File>()
  const [privateKeyFile, setPrivateKeyFile] = useState<File>()
  const [formError, setFormError] = useState('')
  const [selection, setSelection] = useState<Record<string, string>>({})

  const upload = useMutation({
    mutationFn: async () => {
      if (!certificateFile || !privateKeyFile) throw new Error('请选择证书链文件和私钥文件。')
      return protoSend(
        editing ? `/api/operator/certificates/${editing.certificateProfileId}` : '/api/operator/certificates',
        UploadCertificateProfileRequestSchema,
        create(UploadCertificateProfileRequestSchema, {
          certificateProfileId: editing?.certificateProfileId,
          expectedRevision: editing?.revision,
          name,
          certificateChainPem: new Uint8Array(await certificateFile.arrayBuffer()),
          privateKeyPem: new Uint8Array(await privateKeyFile.arrayBuffer()),
        }),
        UploadCertificateProfileResponseSchema,
        editing ? 'PUT' : 'POST',
      )
    },
    onSuccess: () => {
      closeUpload()
      void client.invalidateQueries({ queryKey: ['certificates'] })
      void client.invalidateQueries({ queryKey: ['edges'] })
    },
  })
  const bind = useMutation({
    mutationFn: ({ edgeID, profileID, revision }: { edgeID: string; profileID: string; revision: bigint }) => protoSend(
      `/api/operator/edges/${edgeID}/certificate`,
      BindCertificateProfileRequestSchema,
      create(BindCertificateProfileRequestSchema, { edgeId: edgeID, certificateProfileId: profileID, expectedBindingRevision: revision }),
      BindCertificateProfileResponseSchema,
    ),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['certificates'] })
      void client.invalidateQueries({ queryKey: ['edges'] })
    },
  })

  function openUpload(profile: CertificateProfile | null) {
    setEditing(profile)
    setName(profile?.name ?? '')
    setCertificateFile(undefined)
    setPrivateKeyFile(undefined)
    setFormError('')
    upload.reset()
  }
  function closeUpload() {
    setEditing(undefined)
    setName('')
    setCertificateFile(undefined)
    setPrivateKeyFile(undefined)
    setFormError('')
  }
  function submit(event: FormEvent) {
    event.preventDefault()
    if (!certificateFile || !privateKeyFile) {
      setFormError('证书链和私钥必须同时选择。')
      return
    }
    setFormError('')
    upload.mutate()
  }

  const pending = profiles.isPending || edges.isPending
  const error = profiles.error || edges.error
  return <>
    <PageHeader title="证书" meta="上传当前证书文件，绑定后由 Controller 自动同步到 Edge" actions={<Button tone="primary" onClick={() => openUpload(null)}><Upload size={17} />上传证书</Button>} />
    {pending ? <Skeleton /> : error ? <ErrorState error={error} /> : <div className="certificate-workspace">
      <section className="plain-section">
        <header><div><h2>证书档案</h2><p>档案只保留当前内容；替换后 revision 自动递增</p></div><FileKey2 size={19} /></header>
        {!profiles.data?.profiles.length ? <Empty>尚未上传证书档案</Empty> : <TableFrame><table><thead><tr><th>档案</th><th>DNS 域名</th><th>Revision</th><th>有效期</th><th>绑定状态</th><th>指纹</th><th /></tr></thead><tbody>{profiles.data.profiles.map((profile) => {
          const state = aggregateState(profile.bindings)
          return <tr key={profile.certificateProfileId}>
            <td><strong>{profile.name}</strong><small className="mono">{compactID(profile.certificateProfileId)}</small></td>
            <td>{profile.dnsNames.join(', ') || '-'}</td>
            <td>r{profile.revision.toString()}</td>
            <td>{dateTime(profile.notAfter)}<small>生效：{dateTime(profile.notBefore)}</small></td>
            <td><CertificateState state={state} />{profile.bindings.length > 0 && <small>{profile.bindings.length} 个 Edge</small>}</td>
            <td className="mono" title={profile.sha256Fingerprint}>{compactFingerprint(profile.sha256Fingerprint)}</td>
            <td><Button className="table-button" onClick={() => openUpload(profile)}><RefreshCw size={15} />替换</Button></td>
          </tr>
        })}</tbody></table></TableFrame>}
      </section>
      <section className="plain-section">
        <header><div><h2>Edge 绑定</h2><p>绑定只需设置一次；在线 Edge 立即更新，离线 Edge 重连后同步</p></div><Link2 size={19} /></header>
        {!edges.data?.edges.length ? <Empty>尚未创建 Edge</Empty> : <TableFrame><table><thead><tr><th>Edge</th><th>公网入口</th><th>证书档案</th><th>同步状态</th><th>Desired / Applied</th><th>最近结果</th><th /></tr></thead><tbody>{edges.data.edges.map((edge) => {
          const edgeID = edge.config?.edgeId ?? ''
          const currentProfile = edge.certificate?.certificateProfileId ?? ''
          const selected = selection[edgeID] ?? currentProfile
          return <tr key={edgeID}>
            <td><strong>{edge.config?.name}</strong><small>{edge.config?.region}</small></td>
            <td className="mono">{edge.config?.publicEndpoint}</td>
            <td><select className="input table-select" aria-label={`${edge.config?.name} 的证书档案`} value={selected} onChange={(event) => setSelection({ ...selection, [edgeID]: event.target.value })}><option value="" disabled>请选择证书档案</option>{profiles.data?.profiles.map((profile) => <option key={profile.certificateProfileId} value={profile.certificateProfileId}>{profile.name}</option>)}</select></td>
            <td><CertificateState state={edge.certificate?.syncState} online={edge.runtime?.online} /></td>
            <td>{edge.certificate ? `r${edge.certificate.desiredRevision} / r${edge.certificate.appliedRevision}` : '-'}</td>
            <td>{edge.certificate?.lastErrorMessage || dateTime(edge.certificate?.appliedAt)}</td>
            <td><Button className="table-button" disabled={bind.isPending || selected === currentProfile} onClick={() => bind.mutate({ edgeID, profileID: selected, revision: edge.certificate?.bindingRevision ?? 0n })}>保存</Button></td>
          </tr>
        })}</tbody></table></TableFrame>}
        {bind.error && <div className="section-notice"><Notice tone="error">{bind.error.message}</Notice></div>}
      </section>
    </div>}
    <Dialog title={editing ? `替换 ${editing.name}` : '上传证书'} open={editing !== undefined} onClose={closeUpload} footer={<><Button tone="quiet" onClick={closeUpload}>取消</Button><Button tone="primary" type="submit" form="certificate-upload" disabled={upload.isPending}>{upload.isPending ? '正在校验' : editing ? '替换并自动更新' : '上传证书'}</Button></>}>
      <form id="certificate-upload" className="form-grid" onSubmit={submit}>
        <Field label="档案名称"><Input required maxLength={80} placeholder="例如 中国区 Edge 证书" value={name} onChange={(event) => setName(event.target.value)} /></Field>
        <Field label="证书链文件" hint="选择 PEM 格式的 fullchain.pem"><Input required type="file" accept=".pem,.crt,.cer,application/x-pem-file" onChange={(event) => setCertificateFile(event.target.files?.[0])} /></Field>
        <Field label="私钥文件" hint="选择与证书匹配的 privkey.pem；文件内容不会在页面回显"><Input required type="file" accept=".pem,.key,application/x-pem-file" onChange={(event) => setPrivateKeyFile(event.target.files?.[0])} /></Field>
        <Notice tone="warning">上传后会自动同步所有已绑定 Edge。无效证书不会替换当前在用证书。</Notice>
        {(formError || upload.error) && <p className="form-error">{formError || upload.error?.message}</p>}
      </form>
    </Dialog>
  </>
}

export function CertificateState({ state, online = true }: { state?: CertificateSyncState; online?: boolean }) {
  if (state === CertificateSyncState.APPLIED) return <span className="certificate-state certificate-state-applied"><i />已应用</span>
  if (state === CertificateSyncState.FAILED) return <span className="certificate-state certificate-state-failed"><i />应用失败</span>
  if (state === CertificateSyncState.PENDING) return <span className="certificate-state certificate-state-pending"><i />{online ? '待同步' : '离线待同步'}</span>
  return <span className="certificate-state"><i />未绑定</span>
}

function aggregateState(bindings: CertificateBinding[]): CertificateSyncState | undefined {
  if (!bindings.length) return undefined
  if (bindings.some((binding) => binding.syncState === CertificateSyncState.FAILED)) return CertificateSyncState.FAILED
  if (bindings.some((binding) => binding.syncState !== CertificateSyncState.APPLIED)) return CertificateSyncState.PENDING
  return CertificateSyncState.APPLIED
}

function compactFingerprint(value: string): string {
  return value.length > 20 ? `${value.slice(0, 12)}…${value.slice(-8)}` : value || '-'
}
