# 交付证据

《长任务 Agent 的可靠执行：状态、审批与故障恢复》，作者Aias00。2026-10-21交付。

## 最终版本

- [在线阅读](https://aias00.github.io/durable-agent-workflow-book/)
- [EPUB](https://aias00.github.io/durable-agent-workflow-book/downloads/durable-agent-workflow-book.epub)
- [独立实验ZIP](https://aias00.github.io/durable-agent-workflow-book/downloads/lab.zip)
- [GitHub Release v1.0.0](https://github.com/Aias00/durable-agent-workflow-book/releases/tag/v1.0.0)
- 产品源与tag commit：`6ce0cbd3d713c7723858e8c858fbfb4be48155a8`。
- [完整成功CI 39591665656](https://github.com/Aias00/durable-agent-workflow-book/actions/runs/39591665656)，2026-10-21 11:24:01—11:27:14 UTC，build与deploy均success。
- 同次工件：`book-downloads` ID `12342542534`、`workflow-runtime-evidence` ID `12342412557`、`github-pages` ID `12342257811`。

Release十个附件直接上传成功CI的原始book-downloads，没有本地重建或替换。tag固定指向成功产品源。book/validation.json与book/model-experiment.json随后从同次CI复制为规范运行记录；这些证据和本文件用`[skip ci]`提交，不替换已发布产品。

## 全书与实验包

18章正文、前言、3个手写附录和1个自动完整代码附录，共23篇。排除代码块后正文26718中文汉字，各章超过1200汉字。18章及附录已人工核读；正式来源索引列出10项资料，其中8项官方页面的本地缓存SHA与公开索引逐一相符，另两项固定模型revision和官方引擎发布。原始官方缓存、第三方模型与二进制不公开再分发。

实验ZIP有17个文件，包含7个Go文件、根go.mod/go.sum、嵌入的lab/schema.sql、lab/README.md、模型复现脚本和许可证。解压后模块根即ZIP根。源码与生成代码附录同源；全部ZIP文件与产品源逐字节相等。运行数据库实验须按lab/README.md启动隔离PostgreSQL，并设置BOOK_LAB_DSN；没有数据库时的测试skip不属于集成通过证据。

## 真实Linux证据

Go1.24.3 linux/amd64、PostgreSQL17.11（Debian17.11-1.pgdg13+2），实际容器摘要：

`postgres@sha256:a63c1ea3f6a3a5a1621f97d2aba3b30faecfab4224c1b0000cc85ced57bb1939`

仓库源码与独立ZIP目录分别执行`go test -race -count=1 -json ./...`。两份原始tests.jsonl和archive-tests.jsonl均已重新下载核对：26个顶层测试加2个子例，精确28条pass、零测试skip、零失败。cmd/worker与cmd/plancheck没有测试文件，Go输出的两条包级skip不表示集成跳过；验收仍拒绝任何package/test fail和测试级skip，并要求全部28个名称各通过一次。go vet通过。

原始证据包括：双worker及双真实进程竞争只领取一次；工具事务提交后丢HTTP响应，再权威查询补齐成功；在after_claim和after_tool两个窗口实际发送SIGKILL，等待PostgreSQL时钟中的三秒租约自然到期；无幂等/查询能力时保留unknown；旧围栏拒绝；可信429的真实退避与持久预算；审批期限、取消期间查询、版本拒绝、回执参数匹配、终态和事件共同回滚。精确SQL注入的到期边界与自然等待分别标注，不混用。

同次LinuxCI还实际运行Qwen2.5-0.5B-Instruct GGUF，revision `9217f5db79a29953eb74d5343926648285ec7e67`，491400032字节，SHA256 `74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db`；llama.cpp b11789官方CPU包经过固定SHA校验。两条作者输入分别生成account-a/500和account-b/1200，独立Go准入通过。报告保留原stdout和SHA；运行器只从CLI外壳提取唯一候选JSON，再作严格准入。准入不写数据库、不审批、不执行效果；两个样例不代表业务准确率、模型排名或提示注入免疫。

## 格式与公网校验

同次CI完成完整源码/EPUB/网页逐章文字校验、链接/搜索/ZIP核对、EPUBCheck5.4.0和浏览器检查。最终公网EPUB再次运行EPUBCheck：0 fatals / 0 errors / 0 warnings / 0 infos。

公网Playwright1.64.1验证23篇320/390px均无横向溢出，含全篇无JavaScript阅读、原生目录、翻页、全文搜索、字号持久化、默认浅色、共享深色主题与跨标签同步、禁用存储和下载，无JS异常。命令：

```sh
PLAYWRIGHT_MODULE=/Volumes/data/aias-homepage/node_modules/playwright BOOK_URL=https://aias00.github.io/durable-agent-workflow-book/ node scripts/browser-check.cjs
```

另从公开URL逐篇请求23篇HTML，解析article后与源稿独立Pandoc渲染及EPUB相应章节全文归一比较，全部相等。Pages公开EPUB、离线HTML、完整Markdown、ZIP、validation.json和model-experiment.json均与成功CI原文件逐字节相等；Release EPUB与ZIP实际重新下载后也与同次CI和Pages相等。

| 文件 | SHA256 |
| --- | --- |
| EPUB，156189字节（CI=Pages=Release） | `9e39bce14f69d085244050ea784ee77097b0acb98f1044d1bb1f98e346fc574d` |
| 完整Markdown | `3e32c0d868647c9769c9c89078d94c23e62e5d58423e32046d07088d0b0acb9d` |
| 离线HTML | `83ef8f6e62cc4df1a118663ceed51a5ceb8e846b08f6b86cce713210385a8544` |
| 实验ZIP（CI=Pages=Release） | `560727f591196833fd873efc75386b9011f0d0b853f72c23ce3d41701237cfa6` |
| 真实运行汇总 | `341b4669c44c5c0bff25b4e9065d3dd763f29e1cbdb8e20447b6c2a89843e60e` |
| 真实模型报告 | `b6a10f05e385e59baff792a667d3a65adabd30a1a99b56231d082433eaf3f0cd` |

## 结论边界与复核责任

这是单个教学积分副作用节点。工具账本与工作流共享PostgreSQL实例但各自提交事务；实验没有证明通用工作流平台、恰好一次系统、生产身份认证、全局任务完成期限、多区域高可用或数据库断电恢复。取消不是撤销既有效果；无可信工具合同的unknown需要人工核查。业务数据均为教学构造，开发凭据明确公开，不包含真实用户数据。

本轮审阅先独立只读检查了18章、附录和原始PG/HTTP/进程实验；此后同一执行者接手ZIP验收门槛修复与发布，因此该流水线修改不自称独立审计。另一位go_service_book代理独立逐行审阅门槛及原始Python，正常流通过，八类失败、跳过、缺项、重复或额外测试流均拒绝；最终CI与公开附件由其另行复核。后续产品修订应发布补丁版本，保留v1.0.0原附件与哈希。
