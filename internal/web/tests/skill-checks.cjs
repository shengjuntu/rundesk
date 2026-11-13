/* Native Skill-Up UI: execution consent, bounded imports and honest outcomes. */
'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const {JSDOM} = require('jsdom');
const root = path.resolve(__dirname, '..'), copy = value => JSON.parse(JSON.stringify(value));
const malicious = '<img src=x onerror="window.skillCheckSourceExecuted=true"><script>window.skillCheckSourceExecuted=true</script>';
const pack = {id:'pack-fixed',name:'Research',digest:'sha256:fixed-check-content',files:[{path:'SKILL.md',size:80},{path:'evals/eval.yaml',size:100}]};
const primaryReport = {skillName:'Research',schemaVersion:'v1alpha1',engineName:'codex',modelName:'fixture-model',configurationId:'sha256:config-fixture',identityComplete:true,status:'incomplete',summary:{total:4,pass:1,fail:0,error:1,skip:1,unknown:1,durationMs:200},warnings:['A skipped result is not a pass'],cases:[
  {caseId:'shared',title:malicious,variant:'with_skill',status:'PASS',durationMs:20,assertions:[{text:'Expected literal output',passed:true,skipped:false,evidence:malicious,source:'rule'}]},
  {caseId:'shared',title:'Without skill comparison',variant:'without_skill',status:'FAIL',durationMs:20,assertions:[]},
  {caseId:'broken',title:'Engine failed',variant:'with_skill',status:'ERROR',durationMs:20,error:malicious,assertions:[]},
  {caseId:'skipped',title:'Skipped check',variant:'default',status:'SKIP',durationMs:0,assertions:[]},
  {caseId:'unknown',title:'Unknown status',variant:'default',status:'UNKNOWN',durationMs:0,assertions:[]}
]};
const records = [
  {id:'report-a',packId:pack.id,digest:pack.digest,source:'docker_run',createdAt:'2026-11-12T10:00:00Z',report:copy(primaryReport)},
  {id:'report-b',packId:pack.id,digest:pack.digest,source:'external_unverified',createdAt:'2026-11-12T11:00:00Z',report:copy(primaryReport)}
];
const comparison = {baselineId:'report-a',candidateId:'report-b',comparison:{comparable:true,verdict:'incomplete',summary:{fixed:0,regressed:1,unchanged:0,new:1,removed:1,unknown:1},warnings:['A missing or unknown result is not a pass'],cases:[
  {caseId:'shared',variant:'with_skill',beforeStatus:'PASS',afterStatus:'FAIL',change:'regressed'},
  {caseId:'removed-case',variant:'with_skill',beforeStatus:'PASS',change:'removed'},
  {caseId:'unknown-case',variant:'default',beforeStatus:'ERROR',afterStatus:'UNKNOWN',change:'unknown'},
  {caseId:'new-case',variant:'default',afterStatus:'PASS',change:'new'}
]}};
const flush = () => new Promise(resolve => setImmediate(resolve));
async function until(check, message = 'Skill-Up UI did not settle') { for (let n=0;n<300;n++) { if (check()) return; await flush(); } assert(check(),message); }

function fixture(options = {}) {
  const dom = new JSDOM(fs.readFileSync(path.join(root,'index.html'),'utf8'),{url:'http://localhost/#skills/pack/pack-fixed',runScripts:'outside-only'}), w=dom.window, ctx=dom.getInternalVMContext();
  const $ = selector => w.document.querySelector(selector), $$ = selector => [...w.document.querySelectorAll(selector)], requests=[], timers=new Map(); let timerID=0,current=true;
  w.setTimeout=(fn,delay)=>{timers.set(++timerID,{fn,delay});return timerID;};w.clearTimeout=id=>timers.delete(id);w.setInterval=()=>0;
  w.rdText=value=>value;w.rdFormat=(value,...args)=>value.replace(/\$\{(\d+)\}/g,(_,n)=>args[+n]);
  vm.runInContext(fs.readFileSync(path.join(root,'app.js'),'utf8').split('async function boot()')[0],ctx);
  const state=vm.runInContext('state',ctx);Object.assign(state,{identity:{kind:'administrator'},session:{id:'session-running',status:'running'},sending:true,uploads:[{name:'draft attachment'}]});
  $('#prompt').value='Keep this ordinary conversation draft';
  const jobs=copy(options.jobs || []), saved=copy(options.reports || []); let latestJob;
  w.RunDeskDiagnostics={recent:()=>null,request:async(url,request={})=>{
    const route=url.replace('/api/v1',''),method=request.method||'GET',body=typeof request.body==='string'?JSON.parse(request.body):request.body;
    requests.push({route,method,body});
    if(options.request) {const result=await options.request({route,method,body});if(result!==undefined)return result;}
    if(method==='GET') {
      if(route==='/skill-center/check-engines')return {available:options.available!==false,message:options.available===false?'Docker is unavailable':'Only the selected isolated environment is used',items:[{instanceId:'environment-fixture',name:'Testing environment',image:'sha256:frozen-image',available:options.available!==false}]};
      if(route==='/skill-center/packs/pack-fixed/checks')return {items:copy(jobs)};
      if(route==='/skill-center/packs/pack-fixed/check-reports')return {items:saved.map(row=>({...copy(row),casesOmitted:true,report:{...copy(row.report),cases:[]}}))};
      if(route==='/skill-center/check-reports')return {items:(options.allReports || saved).map(row=>({...copy(row),casesOmitted:true,report:{...copy(row.report),cases:[]}}))};
      if(route.startsWith('/skill-center/check-reports/')) {const id=route.split('/').pop(),record=[...saved,...(options.allReports || [])].find(row=>row.id===id);assert(record,'Unknown fixture report');return copy(record);}
      if(route.startsWith('/skill-center/packs/pack-fixed/check-compare?'))return copy(options.comparison || comparison);
      if(route.startsWith('/skill-center/check-jobs/')) {const id=route.split('/').pop(),job=jobs.find(row=>row.id===id);assert(job,'Unknown fixture job');return copy(job);}
    }
    if(method==='POST') {
      if(route==='/skill-center/packs/pack-fixed/checks') {
        latestJob={id:'check-job-'+(jobs.length+1),packId:pack.id,digest:pack.digest,action:body.action,instanceId:body.instanceId,allowNetwork:body.allowNetwork,status:'queued',createdAt:'2026-11-12T10:00:00Z'};jobs.unshift(latestJob);return {job:copy(latestJob)};
      }
      if(/^\/skill-center\/check-jobs\/[^/]+\/cancel$/.test(route)) {const id=route.split('/').at(-2),job=jobs.find(row=>row.id===id);assert(job);job.status='canceled';return copy(job);}
      if(route==='/skill-center/packs/pack-fixed/check-reports') {const row={id:'report-imported',packId:pack.id,digest:body.digest,source:'external_unverified',createdAt:'2026-11-12T12:00:00Z',report:copy(primaryReport)};saved.unshift(row);return copy(row);}
    }
    throw Error('Unexpected Skill-Up request '+method+' '+route);
  }};
  vm.runInContext(fs.readFileSync(path.join(root,'skill-checks.js'),'utf8'),ctx);
  const target=w.document.createElement('div');w.document.body.append(target);
  const chosen=copy(options.pack || pack),handle=w.RunDeskSkillChecks.mount(target,chosen,{isCurrent:()=>current});
  const findButton=label=>[...target.querySelectorAll('button')].find(node=>node.textContent.trim()===label);
  const checkRequests=()=>requests.filter(row=>row.method==='POST'&&row.route.endsWith('/checks'));
  const importRequests=()=>requests.filter(row=>row.method==='POST'&&row.route.endsWith('/check-reports'));
  const check=(selector,value=true)=>{const node=$(selector);assert(node,'Missing control '+selector);node.checked=value;node.dispatchEvent(new w.Event('input',{bubbles:true}));node.dispatchEvent(new w.Event('change',{bubbles:true}));return node;};
  const setFile=(selector,data,name='result.json',size)=>{const node=$(selector);assert(node,'Missing file input '+selector);const file=new w.File([data],name,{type:'application/json'});Object.defineProperty(file,'text',{value:async()=>data});if(size!==undefined)Object.defineProperty(file,'size',{value:size});Object.defineProperty(node,'files',{value:[file],configurable:true});node.dispatchEvent(new w.Event('change',{bubbles:true}));return file;};
  const tick=async()=>{const entry=[...timers.entries()].find(([,timer])=>timer.delay>0&&timer.delay<5000);assert(entry,'No check job poll is scheduled');timers.delete(entry[0]);await entry[1].fn();await flush();};
  return {dom,w,ctx,state,target,$,$$,requests,timers,jobs,saved,findButton,checkRequests,importRequests,check,setFile,tick,invalidate:()=>{current=false;},dispose:()=>handle?.dispose?.(),get latestJob(){return latestJob;}};
}

async function ready(f) {
  await until(() => !!f.$('#skill-check-engine-note') && f.requests.some(row => row.route === '/skill-center/check-engines') && f.requests.some(row => row.route.endsWith('/check-reports')));
  await flush(); await flush();
}
function select(f, selector, value) {
  const node=f.$(selector);assert(node,'Missing select '+selector);node.value=value;node.dispatchEvent(new f.w.Event('change',{bubbles:true}));return node;
}
function findMatchingButton(f, pattern, scope=f.target) {return [...scope.querySelectorAll('button')].find(node=>pattern.test(node.textContent));}

(async()=>{
  const unavailable=fixture({available:false});await ready(unavailable);unavailable.check('#skill-check-ack');
  assert(unavailable.$('#skill-check-run').disabled,'Missing Docker left Run enabled');
  assert(unavailable.$('#skill-check-validate').disabled,'Missing Docker left Validate enabled');
  assert(unavailable.$('#skill-check-engine-note').textContent.includes('Docker is unavailable'));
  assert.equal(unavailable.checkRequests().length,0);unavailable.dispose();

  const missingSuite=fixture({pack:{...pack,files:[{path:'SKILL.md',size:80}]}});await ready(missingSuite);
  missingSuite.check('#skill-check-ack');
  assert(missingSuite.$('#skill-check-run').disabled,'Missing suite was runnable');
  assert(missingSuite.$('#skill-check-validate').disabled,'Missing suite was validatable');
  assert(missingSuite.target.textContent.includes('evals/eval.yaml'));
  assert(missingSuite.$('#skill-check-import-file'),'Missing suite removed report import');missingSuite.dispose();

  const running=fixture();await ready(running);
  const priorSession=running.state.session,priorUploads=running.state.uploads;
  assert(running.$('#skill-check-run').disabled,'Run did not require execution consent');
  assert(running.$('#skill-check-validate').disabled,'Validate did not require execution consent');
  assert.equal(running.$('#skill-check-network').checked,false,'Network defaulted on');
  select(running,'#skill-check-environment','environment-fixture');running.check('#skill-check-ack');
  await until(()=>!running.$('#skill-check-run').disabled);
  running.$('#skill-check-run').click();running.$('#skill-check-run').click();
  await until(()=>running.checkRequests().length===1 && running.timers.size>0);await running.tick();
  await until(()=>running.requests.some(row=>row.route==='/skill-center/check-jobs/check-job-1'));
  const launched=running.checkRequests()[0].body;
  assert.equal(launched.action,'run');assert.equal(launched.digest,pack.digest);assert.equal(launched.instanceId,'environment-fixture');assert.equal(launched.allowNetwork,false);assert.equal(launched.acknowledgeExecution,true);
  assert(running.$('#skill-check-run').disabled,'Active job allowed duplicate execution');
  const cancel=findMatchingButton(running,/取消/);assert(cancel,'Active job lacks cancel');cancel.click();
  await until(()=>running.jobs[0].status==='canceled' && !running.$('#skill-check-run').disabled);
  assert(running.requests.some(row=>row.method==='POST'&&row.route==='/skill-center/check-jobs/check-job-1/cancel'));
  running.$('#skill-check-validate').click();await until(()=>running.checkRequests().length===2);
  assert.equal(running.checkRequests()[1].body.action,'validate');
  assert.equal(running.state.session,priorSession);assert.equal(running.state.sending,true);assert.equal(running.state.uploads,priorUploads);assert.equal(running.$('#prompt').value,'Keep this ordinary conversation draft');running.dispose();

  const resumed=fixture({jobs:[{id:'persisted-job',packId:pack.id,digest:pack.digest,action:'run',instanceId:'environment-fixture',status:'running',createdAt:'2026-11-12T10:00:00Z'}]});await ready(resumed);
  await until(()=>resumed.timers.size>0);await resumed.tick();
  await until(()=>resumed.requests.some(row=>row.route==='/skill-center/check-jobs/persisted-job'));
  assert.equal(resumed.checkRequests().length,0,'Refresh restarted a persisted job');
  assert(resumed.$('#skill-check-run').disabled);const resumedCancel=findMatchingButton(resumed,/取消/);assert(resumedCancel);resumedCancel.click();
  await until(()=>resumed.jobs[0].status==='canceled');resumed.dispose();

  const pendingCleanup=fixture({jobs:[{id:'cancel-pending',packId:pack.id,digest:pack.digest,action:'run',instanceId:'environment-fixture',status:'running',cleanupPending:true,createdAt:'2026-11-12T10:00:00Z'}]});await ready(pendingCleanup);pendingCleanup.check('#skill-check-ack');
  const pendingCancel=findMatchingButton(pendingCleanup,/取消/);assert(pendingCancel);pendingCancel.click();
  await until(()=>pendingCleanup.jobs[0].status==='canceled'&&pendingCleanup.timers.size>0);
  assert(pendingCleanup.$('#skill-check-run').disabled&&pendingCleanup.$('#skill-check-validate').disabled,'Accepted cancellation released execution before container cleanup');
  assert(pendingCleanup.$('#skill-check-jobs').textContent.includes('正在确认容器清理'),'Pending cleanup was not explained after terminal cancellation');
  const beforeCleanupPoll=pendingCleanup.requests.length;await pendingCleanup.tick();
  assert(pendingCleanup.requests.length>beforeCleanupPoll&&pendingCleanup.timers.size>0,'Canceled job stopped polling before cleanup confirmation');
  assert(pendingCleanup.$('#skill-check-run').disabled,'Pending cleanup poll reopened execution');
  pendingCleanup.jobs[0].cleanupPending=false;await pendingCleanup.tick();
  await until(()=>!pendingCleanup.$('#skill-check-run').disabled&&!pendingCleanup.$('#skill-check-validate').disabled);
  assert.equal(pendingCleanup.checkRequests().length,0,'Cleanup completion started another evaluation');pendingCleanup.dispose();

  let cleaning;
  cleaning=fixture({jobs:[{id:'cleanup-job',packId:pack.id,digest:pack.digest,action:'run',status:'canceled',cleanupError:'container removal is unconfirmed',createdAt:'2026-11-12T10:00:00Z'}],request:({method,route})=>{
    if(method==='POST'&&route==='/skill-center/check-jobs/cleanup-job/cancel'){cleaning.jobs[0].cleanupError='';return copy(cleaning.jobs[0]);}
  }});await ready(cleaning);cleaning.check('#skill-check-ack');
  assert(cleaning.$('#skill-check-jobs').textContent.includes('container removal is unconfirmed'));
  assert(cleaning.$('#skill-check-jobs [role="alert"]'),'Unconfirmed container cleanup was not exposed as an alert');
  assert(cleaning.$('#skill-check-run').disabled&&cleaning.$('#skill-check-validate').disabled,'Terminal status bypassed unconfirmed cleanup');
  cleaning.$('#skill-check-run').click();assert.equal(cleaning.checkRequests().length,0);
  const retryCleanup=findMatchingButton(cleaning,/重试清理/);assert(retryCleanup,'Cleanup failure lacks an explicit retry');retryCleanup.click();
  await until(()=>!cleaning.$('#skill-check-run').disabled&&!cleaning.$('#skill-check-validate').disabled);
  assert(cleaning.requests.some(row=>row.method==='POST'&&row.route==='/skill-center/check-jobs/cleanup-job/cancel'));assert.equal(cleaning.checkRequests().length,0,'Cleanup retry started another evaluation');cleaning.dispose();

  for(const [status,label] of [['success','校验通过'],['failed','校验失败'],['unavailable','校验不可用']]){
    const checked=fixture({jobs:[{id:'validation-'+status,packId:pack.id,digest:pack.digest,action:'validate',status:'completed',createdAt:'2026-11-12T10:00:00Z',validation:{status,cases:3,output:'Validation fixture output',warnings:[malicious]}}]});await ready(checked);
    const history=checked.$('#skill-check-jobs');assert(history.textContent.includes('已结束'));assert(history.textContent.includes(label),'Completed validation lost its independent verdict '+status);assert(history.textContent.includes('用例数：3'));
    assert(!history.querySelector('[data-check-status="pass"]'),'A completed validator was presented as a passing evaluation');assert(!history.textContent.includes('用例通过'));
    assert(history.textContent.includes(malicious));assert(!history.querySelector('img'));assert.equal(checked.checkRequests().length,0);checked.dispose();
  }

  const imported=fixture({pack:{...pack,files:[{path:'SKILL.md',size:80}]}});await ready(imported);
  const raw={skill_name:'Research',schema_version:'v1alpha1',engine_name:'codex',model_name:'fixture-model',case_results:[{case_id:'hello',title:'Hello',status:'PASS',configuration:'with_skill'}]};
  imported.setFile('#skill-check-import-file',JSON.stringify(raw));await flush();
  assert(imported.$('#skill-check-import').disabled,'External import did not require explicit acknowledgement');
  imported.check('#skill-check-import-ack');await until(()=>!imported.$('#skill-check-import').disabled);
  imported.$('#skill-check-import').click();await until(()=>imported.importRequests().length===1);
  const importBody=imported.importRequests()[0].body;assert.equal(importBody.digest,pack.digest);assert.equal(importBody.acknowledgeImport,true);assert.deepEqual(importBody.report,raw);
  await until(()=>imported.$('#skill-check-import-note').textContent.includes('报告已导入'));await flush();await flush();
  imported.setFile('#skill-check-import-file','{}','oversized.json',(4<<20)+1);await flush();imported.check('#skill-check-import-ack');
  assert(/4\s*MiB|过大|超限|too large/i.test(imported.$('#skill-check-import-note').textContent),'Oversized import did not explain its size limit');
  if(!imported.$('#skill-check-import').disabled)imported.$('#skill-check-import').click();await flush();await flush();
  assert.equal(imported.importRequests().length,1,'Oversized report reached the backend');
  imported.setFile('#skill-check-import-file','{ invalid JSON');await flush();await flush();imported.check('#skill-check-import-ack');assert(imported.$('#skill-check-import').disabled,'Malformed JSON became importable');assert.equal(imported.importRequests().length,1);imported.dispose();

  let resolveImport;
  const busyImport=fixture({request:({method,route})=>method==='POST'&&route==='/skill-center/packs/pack-fixed/check-reports'?new Promise(resolve=>{resolveImport=resolve;}):undefined});await ready(busyImport);
  busyImport.setFile('#skill-check-import-file',JSON.stringify(raw));await flush();busyImport.check('#skill-check-import-ack');await until(()=>!busyImport.$('#skill-check-import').disabled);busyImport.$('#skill-check-import').click();await until(()=>!!resolveImport);
  assert(busyImport.$('#skill-check-import-file').disabled,'Import in flight allowed replacing the selected file');assert(busyImport.$('#skill-check-import-ack').disabled,'Import in flight allowed changing its consent');
  const pendingRecord={...copy(records[1]),id:'pending-imported'};busyImport.saved.unshift(pendingRecord);resolveImport(copy(pendingRecord));await until(()=>!busyImport.$('#skill-check-import-file').disabled&&!busyImport.$('#skill-check-import-ack').disabled);busyImport.dispose();

  const shown=fixture({reports:records});await ready(shown);
  await until(()=>shown.$('#skill-check-reports').textContent.length>0);
  select(shown,'#skill-check-report-select','report-a');
  await until(()=>shown.$('#skill-check-report-detail').textContent.includes('ERROR'));
  const detail=shown.$('#skill-check-report-detail');
  for(const value of ['PASS','FAIL','ERROR','SKIP','UNKNOWN'])assert(detail.textContent.includes(value),'Missing distinct case state '+value);
  assert(detail.textContent.includes('启用技能'));assert(detail.textContent.includes('未启用技能'));
  assert.equal(detail.querySelectorAll('tbody > tr').length,5,'Case variants were collapsed or duplicated');assert.equal(detail.querySelector('.skill-check-summary span:first-child strong').textContent,'4','Variants were double-counted as primary cases');
  assert(shown.requests.some(row=>row.route==='/skill-center/check-reports/report-a'),'Summary rows did not load complete case details');
  assert(detail.textContent.includes(malicious),'Untrusted case evidence was not rendered as text');
  assert(!shown.target.querySelector('img'));assert(!shown.target.querySelector('script'));assert.equal(shown.w.skillCheckSourceExecuted,undefined);
  select(shown,'#skill-check-baseline','report-a');select(shown,'#skill-check-candidate','report-b');shown.$('#skill-check-compare').click();
  await until(()=>shown.requests.some(row=>row.route.includes('/check-compare?')) && shown.$('#skill-check-comparison').textContent.includes('removed-case'));
  const compareQuery=new URLSearchParams(shown.requests.find(row=>row.route.includes('/check-compare?')).route.split('?')[1]);assert.equal(compareQuery.get('baseline'),'report-a');assert.equal(compareQuery.get('candidate'),'report-b');
  const compared=shown.$('#skill-check-comparison');for(const value of ['退化','删除','未知','新增'])assert(compared.textContent.includes(value),'Missing change classification '+value);
  assert(compared.textContent.includes('incomplete')||compared.textContent.includes('不完整')||compared.textContent.includes('未完成'));
  shown.dispose();

  const incompatibleValue=copy(comparison);incompatibleValue.comparison.comparable=false;incompatibleValue.comparison.verdict='pass';incompatibleValue.comparison.reason='Configuration identities differ';
  const incompatible=fixture({reports:records,comparison:incompatibleValue});await ready(incompatible);
  select(incompatible,'#skill-check-baseline','report-a');select(incompatible,'#skill-check-candidate','report-b');incompatible.$('#skill-check-compare').click();
  await until(()=>incompatible.$('#skill-check-comparison').textContent.includes('Configuration identities differ'));
  assert(incompatible.$('#skill-check-comparison [data-check-status="incomplete"]'),'Incompatible comparison became a passing result');incompatible.dispose();

  const unknownPass={baselineId:'report-a',candidateId:'report-b',comparison:{comparable:true,verdict:'pass',summary:{fixed:0,regressed:0,unchanged:0,new:0,removed:0,unknown:1},warnings:[],cases:[{caseId:'changed-source',variant:'with_skill',beforeStatus:'PASS',afterStatus:'PASS',change:'unknown',reason:malicious}]}};
  const unknownGap=fixture({reports:records,comparison:unknownPass});await ready(unknownGap);
  select(unknownGap,'#skill-check-baseline','report-a');select(unknownGap,'#skill-check-candidate','report-b');unknownGap.$('#skill-check-compare').click();await until(()=>unknownGap.$('#skill-check-comparison').textContent.includes('changed-source'));
  assert(unknownGap.$('#skill-check-comparison [data-check-status="incomplete"]'),'Unknown summary was presented as passing despite its coverage gap');assert(unknownGap.$('#skill-check-comparison').textContent.includes(malicious),'Case-specific comparability reason was dropped');assert(!unknownGap.$('#skill-check-comparison img'));unknownGap.dispose();

  let resolveComparison;
  const lateComparison=fixture({reports:records,request:({route})=>route.includes('/check-compare?')?new Promise(resolve=>{resolveComparison=resolve;}):undefined});await ready(lateComparison);
  select(lateComparison,'#skill-check-baseline','report-a');select(lateComparison,'#skill-check-candidate','report-b');lateComparison.$('#skill-check-compare').click();await until(()=>!!resolveComparison);
  select(lateComparison,'#skill-check-candidate','report-a');resolveComparison(copy(comparison));await flush();await flush();
  assert.equal(lateComparison.$('#skill-check-comparison').textContent,'','A late comparison populated a changed selection');assert(lateComparison.$('#skill-check-compare').disabled);lateComparison.dispose();

  let resolveRecords;
  const latePage=fixture({request:({route})=>route==='/skill-center/check-reports'?new Promise(resolve=>{resolveRecords=resolve;}):undefined});
  await until(()=>!!resolveRecords);const loadingHTML=latePage.target.innerHTML;latePage.invalidate();resolveRecords({items:copy(records)});await flush();await flush();
  assert.equal(latePage.target.innerHTML,loadingHTML,'A late report list populated another page');latePage.dispose();

  const stale=fixture({jobs:[{id:'stale-job',packId:pack.id,digest:pack.digest,action:'run',status:'running'}]});await ready(stale);await until(()=>stale.timers.size>0);
  const beforeRequests=stale.requests.length,beforeHTML=stale.target.innerHTML;stale.invalidate();
  if(stale.timers.size)await stale.tick();
  assert.equal(stale.requests.length,beforeRequests,'A stale page polled its previous job');assert.equal(stale.target.innerHTML,beforeHTML);stale.dispose();

  console.log(JSON.stringify({passed:true,checks:['Docker availability and suite presence do not imply execution success','execution requires explicit consent with network off by default and a fixed digest','refresh resumes persisted jobs and cancellation without rerun','unconfirmed container cleanup blocks new execution until explicit retry succeeds','validation status stays separate from task completion and evaluation verdict','bounded original Skill-Up JSON import requires unverified-source consent and freezes file/consent while in flight','PASS FAIL ERROR SKIP UNKNOWN and with/without variants remain distinct','comparison preserves regressions, added, removed and unknown outcomes','untrusted report content remains text','stale page polls and responses cannot change another context','ordinary conversation identity and draft are preserved'],scope:'shipped native JavaScript in JSDOM; no Docker/Agent/model or browser geometry'}));
})().catch(error=>{console.error(error);process.exitCode=1;});
