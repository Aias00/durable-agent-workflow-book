# 第 17 章：亲手杀死 worker，再解释每一笔效果

崩溃实验最重要的产物不是一个红色错误，而是一份可以解释的账。进程在哪个窗口消失，哪些事务已经提交，恢复者依据什么继续，工具最终有几笔效果，都必须明确。只观察“重启后跑通”会遗漏最危险的重复执行。

## 17.1 故障钩子只用于稳定定位

测试构建 `cmd/worker` 可执行文件。子进程读取已有 schema，连接真实工具，运行至 `after_claim` 或 `after_tool` 时打印阶段并等待标准输入。父测试看到阶段后发送真正的 SIGKILL，而不是让业务函数主动返回错误。

等待期间心跳仍可延长租约，所以父测试不能从启动时刻猜一个固定睡眠时间。它在杀死进程以后查询 PostgreSQL，等持久 lease_until 不再晚于数据库时钟，再运行 `RecoverExpired`。这一流程验证了真实三秒租约的自然到期。

工具服务和 PostgreSQL 没有随 worker 一起被杀死。实验覆盖工作进程丢失，不覆盖数据库断电、磁盘故障或提供者整体消失。故障注入的边界越精确，结论就越容易判断。

## 17.2 领取之后、调用之前退出

`TestSIGKILLAfterClaimQueries404AndRestarts` 在领取提交后暂停。此时数据库已经是 running，但工具效果为零。SIGKILL 使 owner 消失，租约到期后系统提高 fence，把任务改为 unknown。

为什么不直接放回 ready？恢复者只知道执行者不再持有租约，不知道它是否已经越过网络调用。这个实验选择的窗口确实在调用前，生产恢复者通常没有父测试所掌握的精准知识，因此统一核查更容易维持正确边界。

新进程先查询工具，得到权威 404，再按原键重排并领取。实测结果为：

```json
{"window":"after_claim","signal":"SIGKILL",
 "attempts":2,"effects":1,"post_calls":1,"state":"succeeded"}
```

attempts 为 2 是因为领取已经消耗一次持久预算，即使那次没有发出 POST。它测量的是已获得执行权的尝试次数，不是提供者收到请求的次数。预算的单位需要事先定义，否则故障后很容易为了“补回来”无限增加机会。

## 17.3 工具已经提交、终态尚未保存

`after_tool` 发生在 HTTP 客户端拿到有效回执以后，工作流 Complete 以前。父测试杀死进程时工具账本已有一行，工作流仍为 running。这比单纯杀死一个空闲进程更接近业务上危险的窗口。

租约自然到期后，新进程读到 unknown，并查询同一个业务键。工具返回既有效果及回执，恢复者验证键、指纹、账户、数量和回执标识，再保存 succeeded。

```json
{"window":"after_tool","signal":"SIGKILL",
 "attempts":1,"effects":1,"post_calls":1,"state":"succeeded"}
```

这里没有第二次领取，也没有第二次 POST。效果已经存在，核查完成的是本地认识。不能因为 worker 原先来不及写 succeeded，就说那笔效果曾经“失败后重做”。

## 17.4 缺少工具合同就停下来

同样的两个窗口也针对不支持幂等与权威查询的工具运行。调用前退出的窗口最终效果为零，调用后退出的窗口最终效果为一；两者恢复后都保持 unknown，自动核查返回需要人工处置，新的普通 worker 不自动重放。

```json
{"window":"after_claim","state":"unknown","effects":0,"post_calls":0}
{"window":"after_tool","state":"unknown","effects":1,"post_calls":1}
```

父测试知道前者没有效果，系统却不能借用测试专有知识。没有提供者查询合同，它无法仅凭本地 running 证明哪一个窗口发生过。保留 unknown 不是少实现了一步，而是拒绝把不确定结论伪装成安全重试。

人工处置也应查询独立凭据，核对业务对象与金额，再形成受审计决定。随书内核没有提供“管理员点一下强制重放”的接口，更没有把人工确认等同于外部幂等保障。

## 17.5 空查询也不能刷新预算

`TestUnknown404UsesOriginalKeyAndStopsAtBudget` 在真实 PostgreSQL 里重复领取并显式保存 unknown，每次都通过真实 HTTP 获得权威 404。这个测试构造的是核查边界，不是三次实际进程崩溃。

前两次核查能按原键重排，第三次尝试之后预算用尽。即使查询仍返回不存在，也不能无限重排；任务保持 unknown，等待人工评估。观察值中的 attempts 与 persistent_budget 都是 3，original_key_preserved 为 true，effects 为零。

如果只限制网络重试循环，而租约恢复和空查询另走一套“重新开始”路径，就可能绕过上限。本书把上限保存在 workflows.max_attempts，领取、可信拒绝重试、核查重排都读取同一份持久值，进程重启不能把计数清零。

## 17.6 取消和回执仍要过最后一道关

核查在网络外等待时，用户可能请求取消。`TestCancellationDuringLookupCannotRequeue` 让工具查询处理器先触发取消，再返回 404。恢复事务重新锁定当前工作流并读取取消标记，因此不会根据调用前的旧快照重排。真实结果仍为 unknown，cancel_requested 为 true，效果为零。

如果查询返回的回执指纹不符，完成和核查两个入口都必须拒绝。`TestReceiptFingerprintMismatchCannotCompleteOrReconcile` 已分别检查这两个入口。收到 JSON 不等于拿到本任务的证据；即使回执确实来自某个工具，它也可能属于另一项已批准意图。

每次阅读故障实验，最后应列一张事实表：工作流状态、当前 fence、已消耗预算、取消标记、工具效果数、POST 数和查询结果。只要其中一格依赖猜测，就不应把业务动作自动归为“可以再试”。

**练习。** 把上述两个幂等崩溃窗口的时间线画出来，标出领取事务、工具事务、终态事务与 SIGKILL。再解释一个更困难的窗口：取消恰好发生在 BeforeCall 检查之后、HTTP 发出之前。本内核能保证什么，还需要提供者怎样配合才可以进一步约束？
