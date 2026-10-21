# 官方资料核查摘要

以下为原创核查摘要，不复制第三方文档全文。完整原始网页只保存在被忽略的research/cache中。

## PostgreSQL 17: Explicit Locking

https://www.postgresql.org/docs/17/explicit-locking.html

行级锁与事务结束释放；不据此宣称应用租约会终止旧进程。

## PostgreSQL 17: SELECT

https://www.postgresql.org/docs/17/sql-select.html

SKIP LOCKED会跳过锁定行并形成不一致视图，适用于队列式竞争，不保证公平与严格FIFO。

## PostgreSQL 17: Transaction Isolation

https://www.postgresql.org/docs/17/transaction-iso.html

Read Committed的命令快照与并发更新行为；唯一约束后的读取需结合隔离级别。

## PostgreSQL 17: Date/Time Functions

https://www.postgresql.org/docs/17/functions-datetime.html

clock_timestamp返回调用瞬间的实际时间，与事务起始时刻函数区分。

## Go package context

https://pkg.go.dev/context

取消传播是停止工作的信号，不能自动撤销远程已提交的业务效果。

## Go package database/sql

https://pkg.go.dev/database/sql

Tx与连接池生命周期；本地事务不自动覆盖外部工具事务。

## Amazon SQS visibility timeout

https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-visibility-timeout.html

可见性超时与至少一次投递背景；未使用SQS，未声称重现其服务实现。

## Temporal Workflow Execution

https://docs.temporal.io/workflow-execution

事件历史与恢复/重放的比较背景；随书内核不实现完整Temporal或其确定性重放协议。

## Qwen2.5-0.5B-Instruct-GGUF model repository

https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/tree/9217f5db79a29953eb74d5343926648285ec7e67

固定模型revision、GGUF LFS SHA256与Apache-2.0许可；小样本实际CPU生成不作为模型排名。

## llama.cpp b11789 official release

https://github.com/ggml-org/llama.cpp/releases/tag/b11789

官方CPU运行包及发布资产digest；第三方二进制和模型不随书再分发。
