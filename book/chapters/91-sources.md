# 附录 B：来源、复核方法与结论范围

本书将官方语义、随书实现和真实运行记录分别对照。资料链接用于说明合同，不把框架能力自动归给教学内核。

## PostgreSQL 17: Explicit Locking

[原始资料](https://www.postgresql.org/docs/17/explicit-locking.html)

行级锁与事务结束释放；不据此宣称应用租约会终止旧进程。

## PostgreSQL 17: SELECT

[原始资料](https://www.postgresql.org/docs/17/sql-select.html)

SKIP LOCKED会跳过锁定行并形成不一致视图，适用于队列式竞争，不保证公平与严格FIFO。

## PostgreSQL 17: Transaction Isolation

[原始资料](https://www.postgresql.org/docs/17/transaction-iso.html)

Read Committed的命令快照与并发更新行为；唯一约束后的读取需结合隔离级别。

## PostgreSQL 17: Date/Time Functions

[原始资料](https://www.postgresql.org/docs/17/functions-datetime.html)

clock_timestamp返回调用瞬间的实际时间，与事务起始时刻函数区分。

## Go package context

[原始资料](https://pkg.go.dev/context)

取消传播是停止工作的信号，不能自动撤销远程已提交的业务效果。

## Go package database/sql

[原始资料](https://pkg.go.dev/database/sql)

Tx与连接池生命周期；本地事务不自动覆盖外部工具事务。

## Amazon SQS visibility timeout

[原始资料](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-visibility-timeout.html)

可见性超时与至少一次投递背景；未使用SQS，未声称重现其服务实现。

## Temporal Workflow Execution

[原始资料](https://docs.temporal.io/workflow-execution)

事件历史与恢复/重放的比较背景；随书内核不实现完整Temporal或其确定性重放协议。

## Qwen2.5-0.5B-Instruct-GGUF model repository

[原始资料](https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/tree/9217f5db79a29953eb74d5343926648285ec7e67)

固定模型revision、GGUF LFS SHA256与Apache-2.0许可；小样本实际CPU生成不作为模型排名。

## llama.cpp b11789 official release

[原始资料](https://github.com/ggml-org/llama.cpp/releases/tag/b11789)

官方CPU运行包及发布资产digest；第三方二进制和模型不随书再分发。

## 怎样使用随书证据

真实PostgreSQL版本、镜像摘要、Go版本、源码指纹、逐项测试和BOOK_OBSERVATION在实验工作流的runtime.json与原始tests.jsonl中。出版工作流会重新执行实验，Release直接采用该成功构建的书籍工件。

进程强杀实验等待真实三秒租约到期；部分边界测试显式SQL注入过期。两种方法在观察记录中分别标注，不能混为真实时间测量。

模型实验记录固定GGUF、推理引擎、提示、实际输出与Go准入结果。少量作者构造输入只证明该次输入的结果，不代表业务质量分布、提示注入免疫或模型能力排名。

## 未经本书证明的能力

实验是单工具副作用节点，工具账本与工作流共享实例但使用不同事务；没有实现通用工作流平台、跨区域高可用、生产身份委托、完整人工运营界面或全局任务完成期限。备份、数据库断电与主从切换需要独立演练。
