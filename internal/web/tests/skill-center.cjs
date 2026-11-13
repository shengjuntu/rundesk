/* Trust boundaries and explicit review for the shipped Skill Center UI. */
'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const {JSDOM} = require('jsdom');
const root = path.resolve(__dirname, '..'), copy = value => JSON.parse(JSON.stringify(value));
const malicious = '<img src=x onerror="window.skillSourceExecuted=true"><script>window.skillSourceExecuted=true</script>';
const report = {engine:'builtin',engineVersion:'fixture-1',digest:'sha256:fixed-content',status:'completed',recommendation:'review',findings:[{ruleId:'SHELL_EXEC',file:'scripts/run.sh',line:3,severity:'high',evidence:malicious,reason:'Remote command execution needs review',recommendation:'Review the command and its destination'}],coverage:{filesSeen:3,filesScanned:2,filesSkipped:1,skipped:[{file:'asset.bin',reason:'binary content'}]},warnings:['The report is limited to static rules']};
const pack = {id:'pack-fixed',name:'pdf-reader',description:malicious,sourceUrl:'https://github.com/example/skills/tree/fixed/pdf-reader',revision:'frozen-revision',digest:report.digest,createdAt:'2026-11-12T00:00:00Z',files:[{path:'SKILL.md',size:60},{path:'scripts/run.sh',size:120,executable:true}],scan:report};
const flush = () => new Promise(resolve => setImmediate(resolve));
async function until(check, message = 'UI did not settle') { for (let n = 0; n < 200; n++) { if (check()) return; await flush(); } assert(check(), message); }
const submit = form => form.onsubmit({preventDefault(){}});

function fixture() {
  const dom = new JSDOM(fs.readFileSync(path.join(root, 'index.html'), 'utf8'), {url:'http://localhost/#conversations',runScripts:'outside-only'}), w = dom.window, ctx = dom.getInternalVMContext();
  const $ = selector => w.document.querySelector(selector), $$ = selector => [...w.document.querySelectorAll(selector)], requests = [], timers = new Map(); let timerID = 0, jobStatus = 'queued', installs = 0;
  w.setTimeout = (fn, delay) => { timers.set(++timerID, {fn,delay}); return timerID; }; w.clearTimeout = id => timers.delete(id); w.setInterval = () => 0;
  w.HTMLDialogElement.prototype.showModal = function () { this.open = true; }; w.HTMLDialogElement.prototype.close = function () { this.open = false; this.dispatchEvent(new w.Event('close')); };
  w.rdText = value => value; w.rdFormat = (value, ...args) => value.replace(/\$\{(\d+)\}/g, (_, n) => args[+n]);
  const code = fs.readFileSync(path.join(root, 'app.js'), 'utf8'); vm.runInContext(code.slice(0, code.indexOf('async function boot()')), ctx);
  const state = vm.runInContext('state', ctx);
  w.closeConversationSidebar = () => {}; w.RunDeskLayout = {setView(){}}; w.RunDeskTraceUI = {close(){}};
  w.RunDeskConversations = {open:async () => vm.runInContext('setProductPage("conversation")', ctx)};
  w.RunDeskEnvironments = {scopeLabel:() => '当前运行环境'}; w.renderSettings = async () => {};
  w.RunDeskDiagnostics = {recent:() => null,request:async (url, options = {}) => {
    const route = url.replace('/api/v1', ''), method = options.method || 'GET', body = typeof options.body === 'string' ? JSON.parse(options.body) : options.body;
    requests.push({route,method,body,headers:options.headers});
    if (method === 'GET') {
      if (route === '/skill-center/sources') return {items:[{id:'skillsmp',label:'SkillsMP',description:'Source quota information'}]};
      if (route.startsWith('/skill-center/search?')) return {items:[{name:'PDF reader',description:malicious,url:'https://skills.sh/example/pdf',source:'skills.sh',kind:'skill',canImport:true,importUrl:'https://github.com/example/skills',origins:[{source:'skills.sh',url:'https://skills.sh/example/pdf'},{source:'github',url:'javascript:alert(1)'}],metrics:[{name:'installs',value:0,source:'skills.sh'},{name:'github_stars',value:7,source:'github'}]}],providers:[{source:'skills.sh',status:'ok',count:1},{source:'clawhub',status:'error',message:'ClawHub timed out',count:0}],complete:false,warnings:['One source is unavailable']};
      if (route === '/skill-center/packs') return {items:[copy(pack)]};
      if (route === '/skill-center/packs/pack-fixed') return copy(pack);
      if (route.startsWith('/skill-center/packs/pack-fixed/file?')) return {path:'SKILL.md',text:malicious,truncated:false};
      if (route === '/skill-center/engines') return {items:[{id:'builtin',available:true},{id:'skillspector',available:false,message:'SkillSpector is not configured'}]};
      if (route === '/skill-center/jobs/job-1') return {id:'job-1',status:jobStatus,...(jobStatus === 'completed' ? {report:copy(report)} : {})};
    }
    if (method === 'POST') {
      if (route === '/skill-center/fetch') {
        if (body.url === 'https://github.com/example/skills') throw Object.assign(new Error('Choose one skill'), {status:409,code:'multiple_skills',details:{candidates:[{name:'pdf-reader',url:pack.sourceUrl}]}});
        return {pack:copy(pack),scan:copy(report)};
      }
      if (route === '/skill-center/packs/pack-fixed/scan') return {job:{id:'job-1',status:'queued'}};
      if (route === '/skill-center/jobs/job-1/cancel') { jobStatus = 'canceled'; return {id:'job-1',status:jobStatus}; }
      if (route === '/skill-center/packs/pack-fixed/install') {
        installs++; if (!body.acknowledge) throw Object.assign(new Error('Review this exact content before installing'), {status:409,code:'skill_review_required',details:{report:copy(report),digest:pack.digest}});
        return {receipt:{digest:pack.digest},files:copy(pack.files)};
      }
      if (route.startsWith('/workspaces/workspace/skill-bundles?')) {
        const query = new URLSearchParams(route.split('?')[1]);
        if (query.get('acknowledge') !== '1') throw Object.assign(new Error('Review these uploaded files before importing'), {status:409,code:'skill_review_required',details:{report:copy(report),digest:pack.digest}});
        assert(query.get('reason')?.trim()); return {files:copy(pack.files)};
      }
    }
    throw Error('Unexpected request '+method+' '+route);
  }};
  vm.runInContext(fs.readFileSync(path.join(root, 'applications.js'), 'utf8'), ctx);
  vm.runInContext(fs.readFileSync(path.join(root, 'skill-center.js'), 'utf8'), ctx);
  vm.runInContext(fs.readFileSync(path.join(root, 'skill-bundles.js'), 'utf8'), ctx);
  Object.assign(state, {identity:{kind:'administrator'},instances:[{id:'default',name:'Codex'}],workspaces:[{id:'workspace',name:'Research'},{id:'other',name:'Other project'}],session:{id:'session-running',status:'running'},sending:true});
  $('#instance').replaceChildren(...state.instances.map(row => { const node = w.document.createElement('option'); node.value = row.id; node.textContent = row.name; return node; })); $('#instance').value = 'default';
  $('#workspace').replaceChildren(...state.workspaces.map(row => { const node = w.document.createElement('option'); node.value = row.id; node.textContent = row.name; return node; })); $('#workspace').value = 'workspace';
  const findButton = (target, label) => [...target.querySelectorAll('button')].find(node => node.textContent === label);
  const installRequests = () => requests.filter(row => row.route.endsWith('/install'));
  return {dom,w,ctx,state,$,$$,requests,timers,findButton,installRequests,get installs(){return installs;}};
}

(async () => {
  const f = fixture(), {w,ctx,state,$,$$,requests,findButton,installRequests} = f;
  assert($('#nav-skills'), 'Skill Center must have a native navigation entry');
  assert($('link[href="/skill-center.css"]')); assert($('script[src="/skill-center.js"]'));
  vm.runInContext('setProductPage("conversation")', ctx); assert(!$('#nav-skills').classList.contains('hidden'));
  const existingSession = state.session; state.draftContext = 'default:workspace:session-running'; $('#prompt').value = 'keep this draft'; state.uploads = [{name:'draft attachment'}];
  $('#nav-skills').click(); await until(() => !!$('#skill-center-search'));
  assert.equal(state.view, 'skill-center'); assert.equal(w.location.hash, '#skills/search'); assert.equal($('#nav-skills').getAttribute('aria-current'), 'page');
  assert($('.main-row').classList.contains('hidden')); assert.equal(state.session, existingSession); assert.equal(state.sending, true);
  assert.equal(state.drafts.get('default:workspace:session-running').text, 'keep this draft');

  state.identity = {kind:'user'}; const beforeDenied = requests.length;
  await w.RunDeskSkillCenter.open('search'); assert($('#nav-skills').classList.contains('hidden')); assert($('#skill-center-page').textContent.includes('仅向管理员开放')); assert.equal(requests.length, beforeDenied, 'nonadministrator navigation must not query the skill APIs');
  state.identity = {kind:'administrator'}; await w.RunDeskSkillCenter.open('search');
  $('#skill-center-query').value = 'pdf'; await submit($('#skill-center-search'));
  const search = requests.find(row => row.route.startsWith('/skill-center/search?')); assert(search); assert.equal(new URLSearchParams(search.route.split('?')[1]).get('sources'), 'clawhub,skillsmp,skills.sh');
  assert.equal($$('.skill-center-card').length, 1, 'a failed source removed a successful result'); assert.equal($$('.skill-center-metrics strong')[0].textContent, '0', 'a real zero metric was hidden');
  assert.equal($$('.skill-center-metrics small')[0].textContent, 'skills.sh'); assert.equal($$('.skill-center-metrics small')[1].textContent, 'GitHub');
  assert($('#skill-center-results').textContent.includes('ClawHub timed out')); assert($('#skill-center-results').textContent.includes('当前结果可能不完整')); assert.equal($('[data-skill-state="unscanned"]').textContent, '未检查');
  assert($('.skill-center-description').textContent.includes(malicious)); assert(!$('#skill-center-results img')); assert(!$('#skill-center-results script')); assert(!$('a[href^="javascript:"]'));
  findButton($('.skill-center-card'), '获取并检查').click(); await until(() => !!$('.skill-center-candidates'));
  assert($('.skill-center-candidates').textContent.includes('pdf-reader')); assert.equal(requests.filter(row => row.route === '/skill-center/fetch').length, 1, 'candidate selection must not fetch or install automatically');
  findButton($('.skill-center-candidates'), 'pdf-reader').click(); await until(() => !!$('#skill-center-install') && $('#skill-center-engine-note').textContent.includes('not configured'));
  assert.equal(w.location.hash, '#skills/pack/pack-fixed'); assert(findButton($('#skill-center-page'), 'SkillSpector 静态检查').disabled);
  $('.skill-center-file').click(); await until(() => !!$('.skill-center-preview pre'));
  assert.equal($('.skill-center-preview pre').textContent, malicious); assert(!$('.skill-center-preview img')); assert(!$('.skill-center-preview script')); assert.equal(w.skillSourceExecuted, undefined);
  assert($('#skill-center-report').textContent.includes('asset.bin')); assert($('#skill-center-report').textContent.includes('binary content')); assert.equal($('#skill-center-report pre').textContent, malicious); assert(!$('#skill-center-report img'));

  findButton($('#skill-center-page'), '重新快速检查').click(); await until(() => !!findButton($('#skill-center-job'), '取消检查'));
  assert($('#skill-center-install').disabled, 'an active scan left installation enabled'); assert(!$('#skill-center-install-status').hidden);
  const scan = requests.find(row => row.route.endsWith('/scan')); assert.equal(scan.body.engine, 'builtin'); assert.equal(scan.body.allowLLM, false);
  assert.equal(state.session, existingSession); assert.equal(state.sending, true, 'a background scan changed the ordinary session state');
  findButton($('#skill-center-job'), '取消检查').click(); await until(() => $('#skill-center-job').textContent.includes('已取消'));
  assert(!$('#skill-center-install').disabled, 'a terminal scan left installation disabled'); assert($('#skill-center-install-status').hidden);

  $('#skill-center-install').click(); await until(() => !!$('#skill-center-install-submit'));
  await submit($('#skill-center-install-submit').closest('form')); assert.equal(installRequests().length, 1); assert.equal(installRequests()[0].body.acknowledge, false);
  assert(!$('.skill-center-install-review').hidden); assert($('.skill-center-install-review').textContent.includes('SHELL_EXEC')); assert($('.skill-center-install-review').textContent.includes('asset.bin'));
  assert($('#skill-center-install-name').disabled); assert($('#skill-center-install-scope').disabled); assert($('#skill-center-install-replace').disabled);
  assert($('#skill-center-install-override').disabled); $('#skill-center-install-override').click(); await flush(); assert.equal(installRequests().length, 1, 'risk review retried before explicit acknowledgement');
  $('#skill-center-install-ack').checked = true; $('#skill-center-install-ack').dispatchEvent(new w.Event('change')); assert($('#skill-center-install-override').disabled);
  $('#skill-center-install-reason').value = '   '; $('#skill-center-install-reason').dispatchEvent(new w.Event('input')); assert($('#skill-center-install-override').disabled);
  $('#skill-center-install-reason').value = 'Reviewed command and destination for the intended workspace'; $('#skill-center-install-reason').dispatchEvent(new w.Event('input')); assert(!$('#skill-center-install-override').disabled);
  $('#skill-center-install-override').click(); $('#skill-center-install-override').click(); await until(() => !!$('.skill-center-success'));
  assert.equal(installRequests().length, 2); const accepted = installRequests()[1].body;
  assert.equal(accepted.acknowledge, true); assert.equal(accepted.digest, pack.digest); assert.equal(accepted.workspaceId, 'workspace'); assert.equal(accepted.instanceId, 'default'); assert.equal(accepted.scope, 'project'); assert(accepted.reason.includes('Reviewed command'));
  $('.skill-center-install-dialog').close(); $('#skill-center-install').click(); await until(() => !!$('#skill-center-install-submit'));
  $('#workspace').value = 'other'; await submit($('#skill-center-install-submit').closest('form')); assert.equal(installRequests().length, 2, 'changing workspace silently changed the reviewed installation target');
  assert($('.skill-center-install-dialog .error').textContent.includes('已经改变')); $('.skill-center-install-dialog').close(); $('#workspace').value = 'workspace';
  vm.runInContext('setProductPage("conversation")', ctx); assert(!$('.main-row').classList.contains('hidden')); assert($('#skill-center-page').classList.contains('hidden')); assert.equal(state.session, existingSession); assert.equal($('#prompt').value, 'keep this draft');

  // The existing directory/ZIP importer must offer the same explicit review and
  // resubmit the selected content, rather than skipping the gate or re-reading it.
  for (const kind of ['zip', 'directory']) {
    const file = new w.File([kind === 'zip' ? 'original zip bytes' : '# Skill\n'+malicious], kind === 'zip' ? 'pdf-reader.zip' : 'SKILL.md', {type:kind === 'zip' ? 'application/zip' : 'text/markdown'});
    if (kind === 'directory') Object.defineProperty(file, 'webkitRelativePath', {value:'pdf-reader/SKILL.md'});
    const center = w.RunDeskSkillCenter;
    if (kind === 'directory') w.RunDeskSkillCenter = undefined; // The old importer remains usable without the center module.
    w.fixtureFiles = [file]; vm.runInContext(`showSkillImport(fixtureFiles, ${JSON.stringify(kind)}, "workspace", "default")`, ctx);
    const dialog = $('.skill-import-dialog'), form = $('#skill-bundle-submit').closest('form');
    const uploads = () => requests.filter(row => row.route.startsWith('/workspaces/workspace/skill-bundles?'));
    const before = uploads().length; await submit(form); assert.equal(uploads().length, before + 1);
    assert(!dialog.querySelector('.skill-center-install-review').hidden); assert(dialog.textContent.includes('SHELL_EXEC')); assert(dialog.textContent.includes('asset.bin')); assert(!dialog.querySelector('img'));
    assert($('#skill-bundle-name').disabled); assert($('#skill-bundle-scope').disabled); assert($('#skill-bundle-replace').disabled);
    assert($('#skill-bundle-review-continue').disabled); await submit(form); assert.equal(uploads().length, before + 1, 'Enter silently bypassed the upload gate');
    $('#skill-bundle-review-reason').value = 'Reviewed the uploaded files and their intended use'; $('#skill-bundle-review-reason').dispatchEvent(new w.Event('input')); assert($('#skill-bundle-review-continue').disabled);
    $('#skill-bundle-review-ack').checked = true; $('#skill-bundle-review-ack').dispatchEvent(new w.Event('change')); assert(!$('#skill-bundle-review-continue').disabled);
    $('#skill-bundle-review-continue').click(); $('#skill-bundle-review-continue').click(); await until(() => !$('.skill-import-dialog'));
    assert.equal(uploads().length, before + 2, 'an explicit upload review submitted more than once');
    const first = uploads()[before], acceptedUpload = uploads()[before + 1], initialQuery = new URLSearchParams(first.route.split('?')[1]), acceptedQuery = new URLSearchParams(acceptedUpload.route.split('?')[1]);
    assert.equal(acceptedUpload.body, first.body, 'review did not reuse the exact selected upload body'); assert.equal(acceptedQuery.get('acknowledge'), '1'); assert.equal(acceptedQuery.get('reason'), 'Reviewed the uploaded files and their intended use');
    assert.equal(acceptedQuery.get('scope'), initialQuery.get('scope')); assert.equal(acceptedQuery.get('name'), initialQuery.get('name')); assert.equal(acceptedQuery.get('instanceId'), 'default');
    if (kind === 'zip') { assert.equal(first.body, file); assert.equal(first.headers['Content-Type'], 'application/zip'); }
    else { assert(first.body instanceof w.FormData); assert.equal(first.body.get('pdf-reader/SKILL.md').name, 'SKILL.md'); }
    w.RunDeskSkillCenter = center;
  }
  f.dom.window.close();
  console.log(JSON.stringify({passed:true,checks:['administrator-only native navigation and routes','partial-source results and source-specific zero metrics','untrusted descriptions, links, evidence and source remain text','missing SkillSpector stays unavailable','scan cancellation preserves ordinary session state','install gate requires report review, explicit acknowledgement and reason','installation stays bound to reviewed digest and workspace','conversation visibility and draft are preserved','existing ZIP/directory importers freeze reviewed parameters and reuse exact upload bodies','upload review works without the center module and rejects duplicate submissions'],scope:'shipped browser JavaScript in JSDOM; no browser geometry assertions'}));
})().catch(error => { console.error(error); process.exitCode = 1; });
