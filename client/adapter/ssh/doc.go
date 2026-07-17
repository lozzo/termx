// Package ssh 实现 SSH stdio route attempt adapter。
// 本包只拥有单次 attempt 的 OpenSSH process 与 transport cleanup，不拥有 endpoint
// selection、fallback 或客户端 session 状态。
package ssh
