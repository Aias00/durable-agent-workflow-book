#!/usr/bin/env python3
"""Run a pinned real GGUF model, then admit its untrusted output through Go.
This tiny authored fixture is not a business-quality benchmark.
"""
from pathlib import Path
import argparse,hashlib,json,platform,subprocess,time
from datetime import datetime,timezone
ROOT=Path(__file__).resolve().parents[2]
MODEL_SHA='74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db'
REVISION='9217f5db79a29953eb74d5343926648285ec7e67'
parser=argparse.ArgumentParser();parser.add_argument('--binary',required=True);parser.add_argument('--model',required=True);parser.add_argument('--output',default='book/model-experiment.json');args=parser.parse_args()
model=Path(args.model).resolve();binary=Path(args.binary).resolve()
assert hashlib.file_digest(model.open('rb'),'sha256').hexdigest()==MODEL_SHA
version=subprocess.run([str(binary),'--version'],capture_output=True,text=True,check=True,timeout=30)
assert '11789' in version.stdout+version.stderr
cases=[('account-a',500,'Create a plan to issue 500 CNY minor units to account-a.'),('account-b',1200,'Create a plan to issue 1200 CNY minor units to account-b.')]
results=[]
for account,amount,prompt in cases:
 argv=[str(binary),'-m',str(model),'-ngl','0','--device','none','-t','2','-c','2048','-n','180','--temp','0','--seed','42','--single-turn','--simple-io','--no-display-prompt','--no-show-timings','--color','off','--log-disable','--no-warmup','-sys','Output exactly one JSON object, no explanations. Use these exact keys: version, action, account, amount_minor, currency. version=1, action=issue_credit, currency=CNY. Copy account and amount_minor from the user request. Example shape: {"version":1,"action":"issue_credit","account":"account-a","amount_minor":1,"currency":"CNY"}. Never add approvals, identity or tool capabilities.','-p',prompt]
 started=time.monotonic();run=subprocess.run(argv,capture_output=True,text=True,timeout=180,check=True)
 output=run.stdout;objects=[]
 for pos,char in enumerate(output):
  if char=='{':
   try:
    obj,end=json.JSONDecoder().raw_decode(output[pos:])
    if isinstance(obj,dict) and 'action' in obj:objects.append(obj)
   except json.JSONDecodeError:pass
 assert len(objects)==1,('expected one actual generated proposal',output[-1500:])
 proposal=objects[0]
 trace=ROOT/'.build/model-traces';trace.mkdir(parents=True,exist_ok=True);(trace/(account+'.stdout')).write_text(output);(trace/(account+'.stderr')).write_text(run.stderr)
 admitted=subprocess.run(['go','run','./cmd/plancheck'],input=json.dumps(proposal),text=True,capture_output=True,check=False,cwd=ROOT,timeout=60)
 check=json.loads(admitted.stdout) if admitted.returncode==0 else {'admitted':False,'state':'rejected','reason':'strict Go admission rejected generated proposal'}
 matched=proposal.get('account')==account and proposal.get('amount_minor')==amount
 results.append({'prompt':prompt,'proposal':proposal,'expected_account':account,'expected_amount_minor':amount,'matches_authored_request':matched,'admission':check,'elapsed_seconds':round(time.monotonic()-started,3),'generated_stdout':output,'generated_stdout_sha256':hashlib.sha256(output.encode()).hexdigest()})
 print(json.dumps(results[-1],ensure_ascii=False),flush=True)
report={'tested_at_utc':datetime.now(timezone.utc).isoformat(),'model':'Qwen/Qwen2.5-0.5B-Instruct-GGUF','revision':REVISION,'model_file':model.name,'model_sha256':MODEL_SHA,'model_bytes':model.stat().st_size,'license':'Apache-2.0','engine_release':'b11789','engine_version':(version.stdout+version.stderr).strip(),'engine_binary_sha256':hashlib.file_digest(binary.open('rb'),'sha256').hexdigest(),'platform':platform.platform(),'cpu_only':True,'threads':2,'temperature':0,'seed':42,'schema_constrained':False,'setup_note':'The initial constrained-schema attempt failed in llama.cpp b11789 sampler initialization; this report uses unconstrained model generation and independent strict Go admission.','cases':results,'limitations':['Two author-constructed requests, not a business-quality benchmark or model ranking.','Schema constrains syntax and permitted fields, not semantic correctness.','The CLI wrapper stdout is scanned for exactly one candidate object; only that candidate is sent to strict Go admission.','Go admission is stateless: it does not persist a workflow, approve or execute a business effect.','Workflow crash experiments are separate deterministic tests against real PostgreSQL and HTTP.']}
out=ROOT/args.output;out.parent.mkdir(parents=True,exist_ok=True);out.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
# Semantic mismatches and admission rejects remain visible; never discard them to improve a score.
print('RECORDED: actual CPU GGUF generation and strict Go admission; no business effect executed')
