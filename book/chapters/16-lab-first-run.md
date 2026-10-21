# 第 16 章：第一次运行真实数据库实验

本章先得到一份可以追溯的实验结果，再解释它怎样产生。随书代码没有用字典代替 PostgreSQL，也没有把一次函数返回当作崩溃。测试程序建立隔离 schema，启动真实 HTTP 服务，编译 worker 可执行文件，并在指定实验中创建和杀死操作系统子进程。

## 16.1 先确定环境边界

源码模块为 `github.com/Aias00/durable-agent-workflow-book`。已验证环境是 Go 1.24.3、Linux amd64 和 PostgreSQL 17.11。作者本机没有可直接使用的 Docker 或 PostgreSQL，因此本书的集成结论来自公开 GitHub Actions 运行，不能把本机编译结果说成数据库实验。

本实验使用开发数据库名 book、用户 book、开发密码 book-lab-only，端口为 25432。工具也只允许 HTTP 环回地址，令牌是公开的教学常量。它们方便复现实验，不提供生产身份认证。数据库用户要有创建 schema 的权限，且实例必须专门用于开发或 CI。

每项数据库测试创建 `wf_` 加十六位随机十六进制字符的 schema。创建者负责清理自己的 schema；worker 子进程只打开它，退出时不删除。不要把搜索路径改到业务 schema，也不要借实验脚本对共享数据库执行批量删除。

## 16.2 有 Docker 时在本地启动

镜像不只写一个会漂移的标签，而固定到真实运行中记录的摘要：

```sh
docker run --rm --name durable-workflow-lab \
  -p 127.0.0.1:25432:5432 \
  -e POSTGRES_USER=book \
  -e POSTGRES_PASSWORD=book-lab-only \
  -e POSTGRES_DB=book \
  postgres:17@sha256:a63c1ea3f6a3a5a1621f97d2aba3b30faecfab4224c1b0000cc85ced57bb1939
```

保持这个终端运行，在另一个终端检查 `docker exec durable-workflow-lab pg_isready -U book -d book`。数据库就绪以后，进入仓库根目录，设置连接并运行：

```sh
export BOOK_LAB_DSN='postgres://book:book-lab-only@127.0.0.1:25432/book?sslmode=disable'
go test -race -count=1 -json ./... > tests.jsonl
```

`-race` 检查被实际执行到的 Go 数据竞争，`-count=1` 避免复用测试缓存，`-json` 保存可供机器解析的原始输出。竞态检测通过不能替代数据库不变量证明；它与 SQL 状态、工具账本检查一起构成本次证据。

如果没有设置连接变量，数据库测试会明确 skip。本地命令因此可能以成功状态结束，只表示已执行的部分没有失败。真正的验收还要检查测试清单和 skip 数量，不能只看 shell 退出码。

## 16.3 没有本地数据库时使用公开 CI

仓库的 PostgreSQL workflow lab 工作流为每次集成运行启动独立 PostgreSQL 服务，记录实际 server version 和镜像摘要，再执行同一条 race 测试命令。可以在 GitHub 网页手动运行，也可以使用已经登录的 GitHub CLI：

```sh
gh workflow run lab.yml --repo Aias00/durable-agent-workflow-book --ref main
gh run list --repo Aias00/durable-agent-workflow-book --workflow lab.yml
```

选择自己这次运行的编号，下载名为 `workflow-lab-evidence` 的工件。工件包含 `tests.jsonl`、`runtime.json`、实际 PostgreSQL 版本和实际镜像摘要。不要只复制工作流页面的绿色图标，它不能独立解释哪些实验运行过。

源码冻结提交 `5e07ffe1a0752d51924488dc85fea4c494e8cc3d` 的[公开实跑](https://github.com/Aias00/durable-agent-workflow-book/actions/runs/39586231582)通过。汇总记录 26 个顶层测试及两个子例，共 28 个 pass 条目，零 skip，零 package failure；这不是 28 个彼此独立的顶层实验。

## 16.4 从一次响应丢失开始读结果

先选 `TestCommitReplyLostBecomesUnknownThenReconciled`。HTTP 工具在独立事务提交以后，使用真实 HTTP/1 连接接管关闭连接，丢弃响应。worker 因为没有收到可信回执保存 unknown，再通过权威查询恢复。

```json
{"after_reply_loss":"unknown","after_lookup":"succeeded",
 "effects":1,"post_calls":1}
```

效果数量来自工具账本查询，POST 数量来自真实服务计数。核查没有再次 POST，因此这个结果比“客户端重试后最终成功”提供了更清楚的业务解释。测试服务与工作流共用一个 PostgreSQL 实例，但工具提交不属于工作流终态事务。

想单独阅读人类可读输出，可以运行：

```sh
go test -race -count=1 -v ./lab \
  -run TestCommitReplyLostBecomesUnknownThenReconciled
```

不要把工具服务的 DropResponses 改成普通返回一个 Go 错误来加速实验，那会移除真实 HTTP 不确定窗口。每项故障都应尽量发生在被讨论的边界上。

## 16.5 读懂工件中的证明范围

`runtime.json` 保存源码哈希、提交号、运行地址、实际运行时和各个观察对象。先确认 status 为 PASS，再看 expected_top_level_tests 是否全部出现、missing_tests 是否为空。然后按测试名查找观察值，回到原始流核对断言与输出。

测试名也提示了实验手法。SIGKILL 实验等 PostgreSQL 时钟里的三秒租约自然过期；审批、旧 fence 和终态事务实验有部分边界使用 SQL 显式注入。它们都访问真实数据库，但不能据此把所有时间边界说成经过自然等待验证。

本地运行结束后，确认不再需要这个自己创建的容器，再停止 `durable-workflow-lab`。测试正常结束会清理所拥有的 schema；父测试进程被强行杀死时可能留下随机 schema，需要操作者核对来源后处置。名称格式限制减少误操作机会，不构成跨用户所有权认证。

**练习。** 不修改业务源码，分别运行一次缺少连接变量的测试和一次完整 CI。比较原始输出里的 skip、最终进程退出码与 runtime status，解释为什么它们可能不同。再从一个 BOOK_OBSERVATION 找到它对应的测试、数据库查询与副作用计数来源。
