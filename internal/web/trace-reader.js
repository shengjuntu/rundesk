// A bounded reading projection over the unified observation model. Opening a
// message never reads every source event or changes execution state.
window.RunDeskTraceReader = (() => {
  const t = s => typeof rdText === 'function' ? rdText(s) : s;
  const states = {running:'进行中',completed:'已完成',succeeded:'已完成',recorded:'已记录',pending:'等待审批',accepted:'已允许',declined:'已拒绝',rejected:'已拒绝',failed:'失败',unknown:'记录不完整',expired:'已失效',warning:'告警',interrupted:'中断',canceled:'取消',cancelled:'取消',paused:'暂停',waiting:'等待',replayed:'历史回放'};
  const sources = {host:'RunDesk',codex_app_server:'Codex',kun_sdk:'Kun',observeproxy:'LLM 通信'};
  const signals = {start:'缺少开始记录',end:'缺少结束记录',missing_start:'缺少开始记录',missing_end:'缺少结束记录',missing_identity:'缺少调用标识',ambiguous_pair:'无法唯一配对',run_unassigned:'轮次归属未知',invalid_clock:'时间记录不完整',status_unknown:'状态未知',capture_loss:'采集丢失',truncated:'已截断',incomplete:'记录不完整'};
  const attention = a => ['failed','warning','pending','declined','rejected','paused','waiting','interrupted','unknown','expired'].includes(a.status) || !!a.issues?.length || !!a.missing?.length || a.evidenceTruncated || a.transport && (!a.transport.complete || a.transport.truncated || a.transport.incomplete || a.transport.captureLoss);
  const status = a => t(states[a.status] || a.status || '状态未知');
  const shorten = (s,n=100) => {const chars=Array.from(String(s||''));return chars.length>n?chars.slice(0,n).join('')+'…':chars.join('');};
  const node = (tag,attrs,...children) => el(tag,attrs,...children);
  const action = (label,fn,cls='') => button(t(label),fn,cls);
  const text = (tag,attrs,s) => node(tag,attrs,t(s));
  function inlineText(value,host) {
    // Same safe policy as the conversation: no HTML, remote images or custom schemes.
    const re = /(`([^`]+)`|\*\*([^*]+)\*\*|\[([^\]]+)\]\(([^)]+)\))/g;
    let start=0,m;
    while((m=re.exec(value))){
      host.append(document.createTextNode(value.slice(start,m.index)));
      if(m[2])host.append(node('code',{},m[2]));
      else if(m[3])host.append(node('strong',{},m[3]));
      else {let url;try{url=new URL(m[5],location.href);}catch{}
        if(url&&['http:','https:'].includes(url.protocol))host.append(node('a',{href:url.href,target:'_blank',rel:'noopener noreferrer'},m[4]));
        else host.append(document.createTextNode(m[0]));
      }
      start=re.lastIndex;
    }
    host.append(document.createTextNode(value.slice(start)));
  }
  function prose(value) {
    const out=node('div',{class:'trace-prose'}),lines=String(value||'').split('\n');
    for(let i=0;i<lines.length;){
      const line=lines[i];
      if(!line.trim()){i++;continue;}
      if(/^\s*```/.test(line)){
        const language=line.replace(/^\s*```/,'').trim(),code=[];i++;
        while(i<lines.length&&!/^\s*```/.test(lines[i]))code.push(lines[i++]);
        if(i<lines.length)i++;
        out.append(node('div',{class:'trace-code'},node('small',{},language||t('代码')),node('pre',{},node('code',{},code.join('\n')))));continue;
      }
      if(line.includes('|')&&i+1<lines.length&&/^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[i+1])){
        const cells=s=>s.trim().replace(/^\||\|$/g,'').split('|').map(v=>v.trim()),table=node('table',{}),head=node('tr',{}),body=node('tbody',{});
        for(const cell of cells(line)){const n=node('th',{});inlineText(cell,n);head.append(n);}table.append(node('thead',{},head));i+=2;
        while(i<lines.length&&lines[i].includes('|')&&lines[i].trim()){const row=node('tr',{});for(const cell of cells(lines[i++])){const n=node('td',{});inlineText(cell,n);row.append(n);}body.append(row);}
        table.append(body);out.append(node('div',{class:'trace-table'},table));continue;
      }
      if(/^\s*([-*+] |\d+\. )/.test(line)){
        const ordered=/^\s*\d+\. /.test(line),list=node(ordered?'ol':'ul',{}),pattern=ordered?/^\s*\d+\. /:/^\s*[-*+] /;
        while(i<lines.length&&pattern.test(lines[i])){const li=node('li',{});inlineText(lines[i++].replace(pattern,''),li);list.append(li);}out.append(list);continue;
      }
      const heading=line.match(/^(#{1,6})\s+(.+)$/);
      if(heading){const h=node('h'+Math.min(heading[1].length+2,6),{});inlineText(heading[2],h);out.append(h);i++;continue;}
      const p=node('p',{});inlineText(line,p);i++;
      while(i<lines.length&&lines[i].trim()&&!/^(\s*```|#{1,6} |\s*([-*+] |\d+\. ))/.test(lines[i])){p.append(node('br',{}));inlineText(lines[i++],p);}
      out.append(p);
    }
    return out;
  }
  function blockView(block,expanded=false) {
    if(!expanded&&block.kind==='text'&&Array.from(block.text||'').length>4000){
      const count=Array.from(block.text).length,preview=prose(shorten(block.text,800)),summary=text('summary',{},'展开长内容'),details=node('details',{class:'trace-long-content'},summary),host=node('div',{class:'trace-long-message'},preview,details);
      summary.append(document.createTextNode(' · '+count+' '+t('字符')));
      details.addEventListener('toggle',()=>{preview.hidden=details.open;summary.textContent=t(details.open?'收起长内容':'展开长内容')+' · '+count+' '+t('字符');if(details.open&&details.childElementCount===1)details.append(blockView(block,true));});
      return host;
    }
    if(block.kind==='text')return prose(block.text);
    if(block.kind==='file')return node('div',{class:'trace-file'},text('span',{},'文件'),node('code',{},block.path||block.text||''));
    if(block.kind==='diff'){
      const pre=node('pre',{class:'trace-diff'});
      for(const line of String(block.text||'').split('\n'))pre.append(node('span',{class:line.startsWith('+')?'trace-diff-add':line.startsWith('-')?'trace-diff-remove':'',},line+'\n'));
      return node('div',{class:'trace-code'},node('small',{},block.path||t(block.label||'文件变更')),pre);
    }
    // Keep JSON verbatim: parsing would round large integer tool arguments.
    return node('div',{class:'trace-code'},node('small',{},t(block.label||block.language||(block.kind==='json'?'JSON':'输出'))),node('pre',{},node('code',{},block.text||'')));
  }
  function mount({host,read,scope,session={},onSelectRun=()=>{},onTransport=()=>{},onKun}) {
    let current=typeof scope==='function'?scope():{...scope},closed=false,version=0,listTicket=0,runTicket=0,evidenceTicket=0;
    let offset=0,next=0,hasMore=false,runOffset=0,runNext=0,runMore=false,entries=[],selected='',focusBack=null,loading=false,coverage='';
    const root=node('div',{class:'trace-reader'}),rail=node('aside',{class:'trace-run-nav','aria-label':t('轮次导航')}),runList=node('div',{class:'trace-run-list'}),runStatus=text('span',{class:'trace-run-status'},''),runError=node('p',{class:'error',role:'alert'});
    const content=node('section',{class:'trace-content','aria-label':t('会话轨迹')}),timeline=node('div',{class:'trace-timeline'}),pageStatus=node('span',{class:'trace-page-status',role:'status'}),notice=node('p',{class:'trace-reading-note'}),error=node('p',{class:'error',role:'alert'});
    const drawer=node('aside',{class:'trace-evidence',hidden:true,'aria-label':t('观测证据'),tabindex:'-1'}),drawerBody=node('div',{class:'trace-evidence-body'}),drawerClose=action('收起证据',closeEvidence,'quiet');
    const find=node('input',{type:'search',placeholder:t('搜索本页'),maxLength:1000,'aria-label':t('搜索本页')}),thinking=node('input',{type:'checkbox','aria-label':t('显示推理')}),transport=node('input',{type:'checkbox','aria-label':t('显示通信记录')});thinking.checked=true;
    const prev=action('上一页',()=>loadPage(Math.max(0,offset-40))),more=action('下一页',()=>loadPage(next));
    const earlier=action('更早轮次',()=>loadRuns(Math.max(0,runOffset-50))),later=action('更多轮次',()=>loadRuns(runNext));
    const valid=v=>!closed&&version===v;
    const args=()=>({through:current?.through,...(current?.runId?{runId:current.runId}:{})});
    function closeEvidence(){evidenceTicket++;drawer.hidden=true;root.classList.remove('has-evidence');selected='';timeline.querySelectorAll('.trace-entry').forEach(n=>n.classList.remove('selected'));if(focusBack?.isConnected)focusBack.focus();}
    drawer.append(node('div',{class:'trace-evidence-head'},text('h3',{},'观测证据'),drawerClose),drawerBody);
    drawer.addEventListener('keydown',e=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();closeEvidence();}});
    function renderEntries(){
      const needle=find.value.trim().toLocaleLowerCase();
      const visible=entries.filter(entry=>(thinking.checked||entry.role!=='reasoning')&&(transport.checked||entry.activity?.kind!=='transport')&&(!needle||[entry.title,...(entry.blocks||[]).map(b=>b.text||b.path||'')].join('\n').toLocaleLowerCase().includes(needle)));
      timeline.replaceChildren(...visible.map(entry=>renderEntry(entry)));
      if(!visible.length)timeline.append(text('p',{class:'trace-empty'},entries.length?'本页没有符合显示条件的记录。可调整筛选或翻页。':'此范围还没有可读取的轨迹。'));
      const hiddenHTTP=entries.filter(e=>e.activity?.kind==='transport').length;
      notice.textContent=[t('正文按页读取；搜索仅匹配本页已载入内容。'),!transport.checked&&hiddenHTTP?t('本页已收起通信记录')+' · '+hiddenHTTP:''].filter(Boolean).join(' ');
      notice.title=coverage;
    }
    function renderEntry(entry){
      const a=entry.activity||{},role=entry.role||'system',isTransport=a.kind==='transport',fold=['tool','reasoning','system'].includes(role),needs=attention(a);
      const article=node('article',{class:'trace-entry trace-role-'+role+(needs?' needs-attention':'')+(selected===entry.id?' selected':''),'data-entry-id':entry.id,'data-run-id':a.runId});
      const label=role==='user'?'你':role==='assistant'?(sources[a.source]||'助手'):role==='reasoning'?'推理记录':isTransport?'LLM 通信':role==='tool'?'工具':'运行记录';
      const meta=node('div',{class:'trace-entry-meta'},text('span',{class:'trace-author'},label));
      if(['补充指令','阶段说明'].includes(entry.title))meta.append(text('span',{},entry.title));
      if(a.start||a.end){const date=new Date(a.start||a.end);if(Number.isFinite(date.valueOf()))meta.append(node('time',{dateTime:a.start||a.end,title:date.toLocaleString()},date.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit',second:'2-digit'})));}
      if(needs||!['completed','succeeded','recorded'].includes(a.status))meta.append(node('span',{class:'trace-state'},status(a)));
      if(entry.contentTruncated||entry.contentUnavailable)meta.append(text('span',{class:'trace-state'},entry.contentUnavailable?'内容有缺口':'内容已截断'));
      const evidence=action('查看证据',()=>inspect(entry,evidence),'trace-evidence-button');evidence.setAttribute('aria-label',t('查看证据')+' · '+(entry.title||t(label)));meta.append(evidence);
      const body=node('div',{class:'trace-entry-body'});
      for(const block of entry.blocks||[])body.append(blockView(block));
      if(entry.contentTruncated)body.append(text('p',{class:'trace-content-warning'},'阅读内容已截断；可从来源事件分块读取已保存内容。'));
      if(entry.contentUnavailable)body.append(text('p',{class:'trace-content-warning'},'部分内容无法读取；请检查来源事件和完整性标记。'));
      if(entry.contentPartial)body.append(text('p',{class:'trace-content-warning'},'当前只记录了部分输出；读取最新记录可查看后续内容。'));
      if(needs){const flags=[...(a.issues||[]),...(a.missing||[])];if(a.evidenceTruncated)flags.push('证据引用不完整');if(a.transport?.captureLoss)flags.push('capture_loss');if(a.transport?.truncated)flags.push('truncated');if(a.transport?.incomplete||a.transport&&!a.transport.complete)flags.push('incomplete');
        const unique=[...new Set(flags)];if(unique.length)body.append(node('p',{class:'trace-content-warning'},unique.map(v=>t(signals[v]||states[v]||v)).join(' · ')));
      }
      if(isTransport){const tr=a.transport||{};body.append(text('p',{},'通信记录独立保留；同一轮次不代表与某个模型步骤一一对应。'),node('p',{class:'help'},[tr.method,tr.statusCode,tr.websocket?'WebSocket':null].filter(Boolean).join(' · ')));
        const ref=(a.evidence||[]).find(v=>v.readWith==='llm_request');if(ref)body.append(action('查看请求与响应',()=>onTransport(ref.eventId)));
      }
      if(fold){
        const details=node('details',{class:'trace-action',open:needs}),summary=node('summary',{},node('span',{class:'trace-action-title'},shorten(t(entry.title||a.title||label),160)));
        if(a.durationMs!=null)summary.append(node('small',{},a.durationMs>=1000?(a.durationMs/1000).toFixed(1)+' s':a.durationMs+' ms'));
        if(!needs)summary.append(node('small',{class:'trace-action-state'},status(a)));
        details.append(summary,body);article.append(meta,details);
      }else article.append(meta,body);
      return article;
    }
    function showEvidence(entry,opener){
      const a=entry.activity||{};selected=entry.id;focusBack=opener||null;drawer.hidden=false;root.classList.add('has-evidence');
      timeline.querySelectorAll('.trace-entry').forEach(n=>n.classList.toggle('selected',n.dataset.entryId===selected));
      const raw=node('section',{class:'trace-raw-event'}),buttons=node('div',{class:'debug-event-buttons'});
      const evidence=(a.evidence||[]).filter(e=>Number.isSafeInteger(e.eventId)&&e.eventId>0&&e.eventId<=current.through);
      for(const ref of evidence){
        if(ref.readWith==='llm_request')buttons.append(action('请求 / 响应',()=>onTransport(ref.eventId)));
        else buttons.append(action(t('事件')+' #'+ref.eventId,()=>loadEvent(ref.eventId,0,raw)));
      }
      drawerBody.replaceChildren(node('h4',{},entry.title||a.title||t('来源记录')),node('p',{class:'help'},[sources[a.source]||a.source,a.runId,status(a)].filter(Boolean).join(' · ')),
        text('p',{class:'help'},'原始记录按需读取，以下引用属于当前固定历史。'),
        node('details',{},text('summary',{},'关联标识与完整性'),node('pre',{},JSON.stringify({snapshotThrough:current.through,coverage,activity:a},null,2))),
        ...(a.evidenceTruncated?[text('p',{class:'trace-content-warning'},'证据引用不完整')]:[]),
        buttons,evidence.length?raw:text('p',{class:'help'},'没有可读取的来源引用。'));
      if(onKun&&a.native?.workerSequence)drawerBody.append(action('在 Kun 高级面板检查',()=>onKun(a.native.workerSequence)));
      drawer.focus({preventScroll:true});if(window.innerWidth<=1100)drawer.scrollIntoView?.({block:'nearest'});return raw;
    }
    function inspect(entry,opener){evidenceTicket++;showEvidence(entry,opener);}
    async function loadEvent(eventId,start,raw){
      const v=version,ticket=++evidenceTicket,requestScope=args();raw.replaceChildren(text('p',{role:'status'},'读取事件…'));
      try{
        const r=await read({kind:'event',through:requestScope.through,eventId,offset:start,limit:4000});if(!valid(v)||ticket!==evidenceTicket)return;
        const data=r.data,back=action('上一块',()=>loadEvent(eventId,Math.max(0,start-4000),raw)),forward=action('下一块',()=>loadEvent(eventId,data.nextOffset,raw));back.disabled=start===0;forward.disabled=!data.hasMore;
        raw.replaceChildren(node('h4',{},t('事件')+' #'+eventId),node('p',{class:'help'},t('已脱敏 JSON')+' · '+start+'–'+data.nextOffset+' / '+data.totalCharacters),node('pre',{},data.text),node('div',{class:'debug-pages'},back,forward));
      }catch(err){if(valid(v)&&ticket===evidenceTicket)raw.replaceChildren(node('p',{class:'error',role:'alert'},err.message));}
    }
    async function inspectEvent(eventId){
      if(!Number.isSafeInteger(eventId)||eventId<=0||eventId>current?.through)return;
      const raw=showEvidence({id:'event-'+eventId,title:t('来源事件')+' #'+eventId,activity:{evidence:[{eventId,readWith:'event'}]}},null);return loadEvent(eventId,0,raw);
    }
    async function loadPage(start=0){
      if(closed||current?.through==null)return;const v=version,ticket=++listTicket;loading=true;offset=start;closeEvidence();prev.disabled=more.disabled=true;error.textContent='';timeline.setAttribute('aria-busy','true');
      // Clear immediately: stale content must never appear under a new scope.
      entries=[];timeline.replaceChildren(text('p',{role:'status'},'读取轨迹…'));pageStatus.textContent='';
      try{
        const r=await read({kind:'transcript',...args(),offset:start,limit:40});if(!valid(v)||ticket!==listTicket)return;
        const data=r.data;entries=data.entries||[];next=data.nextOffset;hasMore=data.hasMore;coverage=data.coverage||'';
        if(hasMore&&(!Number.isSafeInteger(next)||next<=start))throw Error(t('分页没有前进，请刷新后重试。'));
        pageStatus.textContent=(data.total?start+1:0)+'–'+next+' / '+data.total;renderEntries();
      }catch(err){if(valid(v)&&ticket===listTicket){entries=[];timeline.replaceChildren();error.textContent=err.message;hasMore=false;}}
      finally{if(valid(v)&&ticket===listTicket){loading=false;timeline.setAttribute('aria-busy','false');prev.disabled=offset===0;more.disabled=!hasMore;}}
    }
    async function loadRuns(start=0){
      if(closed||current?.through==null)return;const v=version,ticket=++runTicket;runOffset=start;earlier.disabled=later.disabled=true;runError.textContent='';
      try{
        const r=await read({kind:'runs',through:current.through,offset:start,limit:50});if(!valid(v)||ticket!==runTicket)return;
        const data=r.data;runNext=data.nextOffset;runMore=data.hasMore;
        const all=action('全部轮次',()=>onSelectRun(''),'trace-run');all.setAttribute('aria-current',current.runId?'false':'true');
        runList.replaceChildren(all,...(data.runs||[]).map((run,index)=>{const b=action('',()=>onSelectRun(run.id),'trace-run');b.dataset.runId=run.id;b.title=run.question||run.id;b.setAttribute('aria-current',current.runId===run.id?'true':'false');b.append(node('small',{},String(start+index+1).padStart(2,'0')+' · '+status(run)),node('span',{},shorten(run.question||run.id,100)));return b;}));
        if(current.runId&&!(data.runs||[]).some(r=>r.id===current.runId))runList.append(node('p',{class:'help'},t('当前轮次不在本页')+' · '+current.runId));
        runStatus.textContent=(data.total?start+1:0)+'–'+runNext+' / '+data.total;
      }catch(err){if(valid(v)&&ticket===runTicket){runList.replaceChildren();runError.textContent=err.message;runMore=false;}}
      finally{if(valid(v)&&ticket===runTicket){earlier.disabled=runOffset===0;later.disabled=!runMore;}}
    }
    for(const input of [find,thinking,transport])input.addEventListener(input===find?'input':'change',()=>{if(!loading)renderEntries();});
    const railDetails=node('details',{class:'trace-run-outline',open:true},text('summary',{},'轮次导航'),runList,runError,node('div',{class:'trace-run-pages'},earlier,runStatus,later));
    const narrow=window.matchMedia?.('(max-width:720px)'),fitRail=()=>{railDetails.open=!narrow?.matches;};fitRail();narrow?.addEventListener('change',fitRail);
    rail.append(railDetails);content.append(node('div',{class:'trace-reading-toolbar'},find,node('label',{},thinking,document.createTextNode(t('显示推理'))),node('label',{},transport,document.createTextNode(t('显示通信记录')))),notice,error,timeline,node('div',{class:'trace-pages'},prev,pageStatus,more));root.append(rail,content,drawer);host.replaceChildren(root);
    async function refresh(){version++;listTicket++;runTicket++;evidenceTicket++;offset=0;closeEvidence();drawerBody.replaceChildren();if(current?.through==null){entries=[];timeline.replaceChildren();runList.replaceChildren();pageStatus.textContent=runStatus.textContent=notice.textContent=error.textContent=runError.textContent='';prev.disabled=more.disabled=earlier.disabled=later.disabled=true;return;}return Promise.all([loadRuns(runOffset),loadPage(0)]);}
    async function setScope(value){const nextScope={...value},changed=current?.through!==nextScope.through||current?.runId!==nextScope.runId;current=nextScope;if(changed){find.value='';runOffset=0;return refresh();}}
    function destroy(){closed=true;version++;listTicket++;runTicket++;evidenceTicket++;narrow?.removeEventListener('change',fitRail);host.replaceChildren();}
    return {refresh,setScope,destroy,inspectEvent};
  }
  return {mount};
})();
