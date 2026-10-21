#!/bin/sh
# Download only fixed official assets; binaries and weights remain in ignored .tools.
set -eu
task_repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$task_repo_root"
mkdir -p .tools/model
case "$(uname -s):$(uname -m)" in
 Darwin:arm64) task_archive=llama-b11789-bin-macos-arm64.tar.gz; task_archive_sha=a85c325b80b3ce0eab2481db4e0ec988386fc0c044c95a0e6c2efea8c6842606 ;;
 Linux:x86_64) task_archive=llama-b11789-bin-ubuntu-x64.tar.gz; task_archive_sha=d5ec57395e369cec744849a5fd629455c814b0c4a0889eba55fca7de054c0636 ;;
 *) echo 'This fixture supports macOS arm64 and Linux x86_64 only.' >&2; exit 1 ;;
esac
verify_file() {
 python3 - "$1" "$2" <<'PY'
import hashlib,sys
from pathlib import Path
p=Path(sys.argv[1])
if not p.exists():sys.exit(1)
with p.open('rb') as f:digest=hashlib.file_digest(f,'sha256').hexdigest()
sys.exit(0 if digest==sys.argv[2] else 1)
PY
}
task_tar=.tools/model/llama-pinned.tar.gz
if ! verify_file "$task_tar" "$task_archive_sha"; then
 curl --fail --location --silent --show-error --retry 3 --max-time 180 "https://github.com/ggml-org/llama.cpp/releases/download/b11789/$task_archive" -o "$task_tar.tmp"
 verify_file "$task_tar.tmp" "$task_archive_sha"
 mv "$task_tar.tmp" "$task_tar"
fi
task_gguf=.tools/model/qwen2.5-0.5b-instruct-q4_k_m.gguf
task_gguf_sha=74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db
if ! verify_file "$task_gguf" "$task_gguf_sha"; then
 curl --fail --location --silent --show-error --retry 3 --max-time 300 'https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/9217f5db79a29953eb74d5343926648285ec7e67/qwen2.5-0.5b-instruct-q4_k_m.gguf' -o "$task_gguf.tmp"
 verify_file "$task_gguf.tmp" "$task_gguf_sha"
 mv "$task_gguf.tmp" "$task_gguf"
fi
python3 - "$task_tar" <<'PY'
from pathlib import Path
import tarfile,sys
with tarfile.open(sys.argv[1]) as tar:tar.extractall('.tools/model/runtime',filter='data')
files=list(Path('.tools/model/runtime').rglob('llama-cli'))
assert len(files)==1
print('Verified binary:',files[0])
print('Verified GGUF: .tools/model/qwen2.5-0.5b-instruct-q4_k_m.gguf')
PY
