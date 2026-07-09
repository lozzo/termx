const GITHUB_AUTH_ERROR_MESSAGES: Record<string, string> = {
  github_not_configured: "GitHub 登录尚未配置完成，请联系管理员。",
  github_access_denied: "您已取消 GitHub 授权。",
  github_oauth_failed: "GitHub 登录失败，请稍后重试。",
  github_state_invalid: "GitHub 登录状态校验失败，请重新发起登录。",
  github_token_exchange_failed: "GitHub 授权码换取失败，请稍后重试。",
  github_profile_fetch_failed: "获取 GitHub 用户信息失败，请稍后重试。",
  github_email_fetch_failed: "获取 GitHub 邮箱失败，请稍后重试。",
  github_email_unavailable: "GitHub 账户没有可用的已验证邮箱，无法登录。",
  github_account_conflict: "该邮箱已绑定其他 GitHub 账号，请先处理账户绑定关系。",
  github_username_conflict: "自动生成用户名失败，请稍后重试。",
};

export function getGithubAuthErrorMessage(code?: string | null): string {
  if (!code) return "";
  return GITHUB_AUTH_ERROR_MESSAGES[code] || "GitHub 登录失败，请稍后重试。";
}
