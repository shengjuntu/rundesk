"use strict";
// Presentation only. Recorded lifecycle IDs are kept separate from unknown or
// conflicting native turns; no journal record is rewritten by folding.
window.RunDeskConversationRounds = (() => {
  const terminal=new Set(['completed','succeeded','failed','interrupted','canceled','cancelled','stopped']);
  const failed=new Set(['failed','interrupted','canceled','cancelled','stopped']);
  const preferences=new Map();
  const identifier=value=>typeof value==='string'&&value.length>0&&value.length<=256&&!/[\r\n\x00]/.test(value)?value:'';
  const turnId=event=>identifier(event?.data?.params?.turnId||event?.data?.params?.turn?.id||event?.data?.turnId);
  function title(input) {
    const text=String(input?.text||'').split(/\r?\n/).find(line=>line.trim())||'';
    const chars=[...text.replace(/\s+/g,' ').trim()];return chars.slice(0,120).join('')+(chars.length>120?'…':'')||(input?.files?.length?'附件任务':'未记录用户输入');
  }
  function project(events,session={}) {
    const runs=new Map(),aliases=new Map(),runTurns=new Map(),owners=new Map(),runOwners=new Map(),origins=new Map(),active=new Set();
    const run=(id,index=Number.MAX_SAFE_INTEGER)=>{if(!runs.has(id))runs.set(id,{id,title:'未记录用户输入',status:'unknown',association:'observed',hasInput:false,order:index});const value=runs.get(id);value.order=Math.min(value.order,index);return value;};
    function alias(tid,rid) {if(!tid||!rid)return;if(!owners.has(tid))owners.set(tid,new Set());owners.get(tid).add(rid);if(!runOwners.has(rid))runOwners.set(rid,new Set());runOwners.get(rid).add(tid);}
    // A late host acknowledgement can bind earlier native events. More than
    // one host owner is a conflict, not permission to pick the latest run.
    for(const event of events){const rid=identifier(event?.data?.runId),tid=turnId(event);if(event.direction==='internal'&&rid&&tid)alias(tid,rid);}
    if(!owners.has(identifier(session.turnId))&&!runOwners.has(identifier(session.runId)))alias(identifier(session.turnId),identifier(session.runId));
    const conflictedRuns=new Set([...runOwners].filter(([,ids])=>ids.size>1).map(([id])=>id));
    const conflictedTurns=new Set([...owners].filter(([,ids])=>ids.size>1||[...ids].some(id=>conflictedRuns.has(id))).map(([id])=>id));
    for(const [tid,ids] of owners)if(ids.size===1&&!conflictedTurns.has(tid)){const rid=[...ids][0];aliases.set(tid,rid);runTurns.set(rid,tid);}
    for(let index=0;index<events.length;index++) {
      const event=events[index],data=event?.data||{},params=data.params||{},tid=turnId(event),explicit=identifier(data.runId);
      let rid=event.method==='run/turn'&&event.direction!=='internal'?'':explicit,association='observed';
      if(!rid&&tid){rid=aliases.get(tid)||'';association='reconstructed';
        if(!rid&&!owners.has(tid)&&active.size===1&&['turn/started','item/started'].includes(event.method)){
          const candidate=[...active][0];
          if(!runTurns.has(candidate)&&!conflictedRuns.has(candidate)){rid=candidate;aliases.set(tid,rid);runTurns.set(rid,tid);association='lifecycle';}
        }
      }
      // A conflicting explicit turn remains unassigned even if an item claims
      // a run too. Host input/state records without a turn retain their ID.
      if(tid&&conflictedTurns.has(tid))rid='';
      if(rid){const value=run(rid,index);origins.set(event.id,{runId:rid,association});
        if(event.method==='run/input'){value.title=title(data.input);value.titleGenerated=!String(data.input?.text||'').trim();value.hasInput=true;value.status='running';active.add(rid);}
        if(association==='lifecycle')value.association='lifecycle';
        if(event.method==='run/state'&&typeof data.status==='string'){value.status=data.status;if(terminal.has(data.status))active.delete(rid);}
        if(event.method==='turn/completed'){value.status=params.turn?.status||'completed';active.delete(rid);}
        if(event.method==='approval/pending')value.status='waiting';
        if(event.method==='run/retry')value.retrying=!!data.willRetry;
        if(data.error||event.method==='error'||params.item?.status==='failed'||params.item?.status==='declined')value.attention=true;
      }
    }
    const current=runs.get(identifier(session.runId));
    if(current&&!terminal.has(current.status)&&typeof session.status==='string')current.status=session.status;
    return {runs,origins};
  }
  function readPreferences(sessionId) {
    if(preferences.has(sessionId))return preferences.get(sessionId);
    const result=new Map();
    try{const raw=localStorage.getItem('rundesk-round-disclosure:'+sessionId)||'{}';if(raw.length<=262144){const values=JSON.parse(raw);if(values&&typeof values==='object'&&!Array.isArray(values))for(const [key,value] of Object.entries(values).slice(-2000))if(typeof value==='boolean')result.set(key,value);}}catch{}
    preferences.set(sessionId,result);return result;
  }
  function remember(sessionId,runId,open) {
    const prefs=readPreferences(sessionId);prefs.delete(runId);prefs.set(runId,open);while(prefs.size>2000)prefs.delete(prefs.keys().next().value);
    try{localStorage.setItem('rundesk-round-disclosure:'+sessionId,JSON.stringify(Object.fromEntries(prefs)));}catch{}
  }
  const english={'附件任务':'Task with attachments','未记录用户输入':'User input not recorded','已完成':'Completed','进行中':'Running','正在开始':'Starting','待开始':'Pending','等待处理':'Awaiting action','已暂停':'Paused','失败':'Failed','已中断':'Interrupted','已取消':'Canceled','已停止':'Stopped','记录不完整':'Incomplete record','正在重试':'Retrying','需关注':'Needs attention','未能确定轮次的记录':'Records without a confirmed round','当前仅显示最近 200 条消息记录；较早内容可在轨迹中查看。':'Only the latest 200 message records are shown. Earlier content is available in the trace.','查看轨迹':'View trace','此轮根据输入与原生开始记录重建关联；缺少直接运行标识。':'This round was reconstructed from input and native start records; a direct run association was not recorded.'};
  const text=value=>document.documentElement.lang==='en'&&english[value]?english[value]:window.rdText?rdText(value):value;
  function node(tag,className,content) {const result=document.createElement(tag);if(className)result.className=className;if(content!=null)result.textContent=content;return result;}
  function status(value) {
    const labels={completed:'已完成',succeeded:'已完成',running:'进行中',starting:'正在开始',idle:'待开始',waiting:'等待处理',paused:'已暂停',failed:'失败',interrupted:'已中断',canceled:'已取消',cancelled:'已取消',stopped:'已停止',unknown:'记录不完整'};
    return text(value.retrying?'正在重试':labels[value.status]||value.status)+(value.attention&&!failed.has(value.status)?' · '+text('需关注'):'');
  }
  function reconcile(box,entries,projection,session,partial=false) {
    const focus=document.activeElement,keepFocus=focus&&box.contains(focus),prefs=readPreferences(session.id);
    const existing=new Map([...box.children].filter(n=>n.dataset.roundKey).map(n=>[n.dataset.roundKey,n]));
    const top=[],groups=new Map();let unknown=null;
    if(partial){let notice=box.querySelector(':scope > .conversation-history-limit');if(!notice){notice=node('p','conversation-history-limit');notice.append(document.createTextNode(text('当前仅显示最近 200 条消息记录；较早内容可在轨迹中查看。')));const link=node('button','quiet',text('查看轨迹'));link.type='button';link.onclick=()=>window.RunDeskTraceUI?.open();notice.append(link);}top.push(notice);}
    for(const entry of entries) {
      const info=projection.origins.get(entry.eventId),value=info&&projection.runs.get(info.runId);
      if(!value){
        if(!unknown){const key='unknown:'+session.id+':'+entry.node.dataset.messageKey;unknown=existing.get(key)||node('section','conversation-unassigned');unknown.dataset.roundKey=key;if(!unknown.firstElementChild)unknown.append(node('p','conversation-unassigned-title',text('未能确定轮次的记录')),node('div','conversation-unassigned-body'));top.push(unknown);groups.set(unknown,[]);}
        groups.get(unknown).push(entry.node);continue;
      }
      unknown=null;let details=groups.get(value.id);
      if(!details){
        const key='run:'+session.id+':'+value.id;details=existing.get(key)||node('details','conversation-round');details.dataset.roundKey=key;details.dataset.runId=value.id;details.dataset.status=value.status;
        if(!details.firstElementChild){const summary=node('summary','conversation-round-summary');summary.append(node('span','conversation-round-title'),node('span','conversation-round-status'));details.append(summary,node('div','conversation-round-body'));
          summary.addEventListener('click',event=>{event.preventDefault();details.open=!details.open;details._focusPinned=false;details.dataset.manualOpen=String(details.open);remember(session.id,value.id,details.open);});details.lastElementChild.addEventListener('focusin',()=>{details._focusPinned=true;});}
        const summary=details.firstElementChild;const heading=value.hasInput&&!value.titleGenerated?value.title:text(value.title);summary.firstElementChild.textContent=heading;summary.lastElementChild.textContent=status(value);summary.title=heading+' · '+status(value);
        if(details.contains(focus)&&focus!==summary)details._focusPinned=true;
        const saved=prefs.get(value.id),open=details._focusPinned|| (saved===undefined?!['completed','succeeded'].includes(value.status):saved);
        // Attention and active state stay visible in the summary even when a
        // reader deliberately closes an active disclosure.
        if(details.open!==open)details.open=open;details.dataset.manualOpen=String(saved===true||details._focusPinned===true);details.dataset.status=value.status;
        details.dataset.association=value.association;groups.set(value.id,details);const content=[];if(value.association==='lifecycle'){const note=details.lastElementChild.querySelector('.conversation-round-association')||node('p','conversation-round-association');note.textContent=text('此轮根据输入与原生开始记录重建关联；缺少直接运行标识。');content.push(note);}groups.set(details,content);top.push(details);
      }
      groups.get(details).push(entry.node);
    }
    for(const [host,children] of groups){if(typeof host==='string')continue;const body=host.lastElementChild;const keep=new Set(children);for(const child of [...body.children])if(!keep.has(child))child.remove();children.forEach((child,index)=>{if(body.children[index]!==child)body.insertBefore(child,body.children[index]||null);});}
    const keep=new Set(top);for(const child of [...box.children])if(!keep.has(child))child.remove();top.forEach((child,index)=>{if(box.children[index]!==child)box.insertBefore(child,box.children[index]||null);});
    if(keepFocus&&focus.isConnected&&document.activeElement!==focus)focus.focus?.({preventScroll:true});
  }
  return {project,reconcile};
})();
