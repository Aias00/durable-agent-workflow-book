# 长任务 Agent 的可靠执行：状态、审批与故障恢复

18章中文正文、前言和4个附录，围绕单个业务副作用节点，学习跨进程的状态、审批、领取、租约、围栏、幂等、核查和恢复。正文超过26000中文汉字，完整源码自动生成代码附录。

- [在线阅读](https://aias00.github.io/durable-agent-workflow-book/)
- [EPUB](https://aias00.github.io/durable-agent-workflow-book/downloads/durable-agent-workflow-book.epub)
- [版本发布](https://github.com/Aias00/durable-agent-workflow-book/releases)

## 真实实验

实验使用真实PostgreSQL17.11、Go1.24.3、HTTP工具和独立worker子进程。26个顶层测试与2个子例在[独立实验CI39586231582](https://github.com/Aias00/durable-agent-workflow-book/actions/runs/39586231582)全部通过，28pass、0skip；完整出版CI会对同一组实验再验收。

真实SIGKILL分别落在领取后、工具提交后，等待真实三秒数据库租约到期。响应丢失通过工具事务提交后关闭HTTP连接制造。核查按原操作键查询权威账本，旧围栏无法提交；不支持幂等/查询的工具停在unknown。其他测试覆盖持久预算、真实退避计时、审批过期、取消竞争、回执匹配、版本拒绝与终态/事件事务原子性。

工具和工作流共享实验PostgreSQL实例，使用不同事务。实验不提供通用Temporal、恰好一次系统、生产身份认证、全局任务完成期限、多节点高可用或数据库断电恢复证明。每项边界在正文和实际观察中明确标注。

[实验说明](lab/README.md)包含隔离实例、公开开发密码、随机schema清理范围、独立进程和逐项命令。不配置数据库时集成测试会明确跳过，不能把本地编译绿色当作完整运行证据。

## 实际模型与无状态准入

另有固定revision/LFS指纹的Qwen2.5-0.5B GGUF和llama.cpp b11789实际CPU演示。两条作者构造输入的原stdout、提取的候选JSON与Go严格准入均保留在[模型报告](book/model-experiment.json)。Go准入不持久化、不审批、不执行效果；模型质量与工作流保证分开验证，不以小样本声称业务准确率。

复现与约束说明见[experiments/model/README.md](experiments/model/README.md)。模型和第三方二进制不随本书分发，下载时核对固定SHA。

## 阅读与出版

正文位于book/chapters，默认浅色、共享低饱和深色，可分章、搜索、调字号、移动端与无JavaScript阅读。所有格式由同一Markdown生成，完整文字一致性、链接、实验包和EPUBCheck均设验收门槛。

```sh
python3 scripts/build.py
python3 scripts/verify.py
sh scripts/epubcheck.sh
python3 -m http.server 8784 --bind 127.0.0.1 --directory site
```

需要Python、Pandoc3.12.1；EPUBCheck需要Java17+。实际小模型脚本需Python3.11+；Go实验需Go1.24.3。更多说明见scripts/README.md。

Pages工作流重跑真实数据库/进程实验、实际CPU规划与独立准入、完整书籍构建、实验ZIP解压后的真实测试、EPUBCheck和全章浏览器验收，再部署。Release只采用同次成功CI工件，不独立重建EPUB。

原创文字、图形与教学代码署名Aias00，保留权利；个人学习与运行授权见LICENSE。资料核查摘要见research/sources.md，官方文档、模型、引擎和依赖保留各自权利，原始网页缓存不公开。
