# 实际小模型规划与无状态准入

固定 Qwen2.5-0.5B-Instruct-GGUF revision `9217f5db79a29953eb74d5343926648285ec7e67`，Q4_K_M 文件 491400032 字节，SHA256 `74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db`；llama.cpp 官方 b11789。模型许可 Apache-2.0，引擎保留自身许可；不把模型和第三方二进制装进本书实验包。

```sh
sh experiments/model/fetch.sh
python3 experiments/model/run.py \
  --binary .tools/model/runtime/llama-b11789/llama-cli \
  --model .tools/model/qwen2.5-0.5b-instruct-q4_k_m.gguf
```

需要 Go、Python 3.11+、curl；抓取脚本适配 Linux x86_64/macOS arm64，逐个校验官方资产digest和模型LFS SHA。默认CPU两线程、固定温度/种子；两条作者构造输入只演示规划与准入，不作为质量基准或模型排名。量化模型、引擎或硬件改变后输出可能变化；脚本保留语义不匹配与准入拒绝，不把它们删掉改善指标。

llama-cli 的 stdout 含界面外壳，脚本从中寻找唯一候选JSON，再交给 `cmd/plancheck` 的Go严格准入。报告保留原stdout与SHA。准入器不连接数据库，不创建持久工作流，不批准、不执行积分，只返回 `next_required_stage=persist_then_wait_for_approval`。独立的PostgreSQL/HTTP/进程故障实验见lab/README。

早期试配中，b11789的JSON schema约束发生sampler初始化错误，首次未完整指定键名的提示也被Go准入拒绝。正式复现脚本使用明确字段提示、不约束解码、独立Go准入，setup_note如实保留这个选择。proposal-schema.json只列结构参考，不作为引擎约束启用。

输出 `book/model-experiment.json` 包含模型指纹、实际引擎版本/二进制指纹、提示、原输出、候选对象和准入结果。原始模型、下载缓存和本地诊断保存在忽略目录。
