# 全局 `/goal` 驱动提示词

把下面整段作为一个新的全局目标提交：

```text
/goal

持续推进 /Users/lozzow/Documents/workdir/termx 的 Muxvia 当前活动主线，直到 workflow.md 排定的切片全部完成，或遇到按仓库规则必须停止的真实阻塞。

执行规则：
1. 每轮必须先完整读取仓库根目录 AGENTS.md 与 workflow.md；workflow.md 是范围、先后顺序、完成条件、测试准入和提交规则的唯一活动真值，不得使用聊天旧顺序覆盖它。
2. 本目标明确授权接管当前工作区中已经识别的 CREEM001 本地 checkpoint。第一轮先检查其 diff 和来源，重跑 workflow.md 已声明的本地门禁，确认没有 secret、没有线上部署、CREEM001 仍为“待后置收口”，然后用独立中文提交保存该 checkpoint。不得把这些支付改动混入 OPSHUB001。
3. checkpoint 清理后，严格按 OPSHUB001 -> OPSUSER001 -> OPSREL001 -> OPSE2E001 -> CREEM001 -> ENROLLUX005 -> PG004 -> CLOUDP007 推进。OPSCAT001 与 OPSCOM001 已完成，只做不回归验证，不重新设计。
4. 九模块指用户管理、订单管理、订阅管理、套餐管理、Hub 管理、Agent 概览、版本管理、优惠码管理、用户特权管理。可以参考 ../tgent 的列表、筛选、详情、状态、操作和审计场景，但禁止复制其 Next.js/ORM DTO、数据库 online、直接改 paid、硬删除或其它与 Muxvia truth owner 冲突的实现。
5. 每个切片只实现 workflow.md 声明的最小真实纵向闭环：先 Proto 和 compatibility harness，再领域/事务、Controller/Edge、Web UI，最后真实 PostgreSQL、独立进程与 Playwright/Android 门禁。不得跨切片提前建设未来能力。
6. 每个切片开始时标记“进行中”；通过全部准入并写完证据后才标记“已完成”，使用中文提交信息单独提交，然后自动进入下一切片。不要在普通实现细节上等待用户确认。
7. 发现问题先证明它属于当前切片；只处理可由代码链路、契约或最小复现证明的问题。纯未来扩展、理论 hardening 和跨切片建议记录为 deferred item，不扩大当前实现。
8. 当前切片如被真实外部条件阻塞，把缺失条件、已完成证据、复现命令和恢复入口写入对应文档，然后按 AGENTS.md 停止并报告。尚未轮到的后置条件不能阻塞当前工作，例如 Creem Product ID、Webhook secret 和 sandbox 事件在 OPSE2E001 完成前只保留记录，不允许提前部署支付。
9. 不写入、输出或提交任何 API key、Webhook secret、数据库凭据或私钥；不恢复 archive/legacy/plugin/Web terminal 路径；不覆盖用户或其它代理的未知改动。
10. 每轮持续到当前切片完成并提交；只在真实阻塞、必须的外部授权或 workflow.md 已完成时结束。最终报告已完成切片、提交、测试证据、剩余阻塞和下一行。
```
