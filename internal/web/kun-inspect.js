// Pure event association: IDs are only meaningful inside a run and model step.
// Missing request/response records remain missing; no outcome is inferred.
function kunCallRows(events){
 const rows=new Map();
 for(const event of events){
  const p=event.data||{},d=p.data||{},type=event.method;
  let kind,id,first;
  if(/^kun\/model\.(started|completed)$/.test(type)){kind="model";id=d.step;first=type.endsWith("started");}
  else if(/^kun\/tool\.(started|completed)$/.test(type)){kind="tool";id=[d.step,d.call?.id];first=type.endsWith("started");}
  else if(/^kun\/mcp\.(request|response)$/.test(type)){kind="mcp";id=[d.server,d.exchangeId];first=type.endsWith("request");}
  else continue;
  const key=JSON.stringify([p.runId,kind,id]);
  let row=rows.get(key);if(!row){row={key,kind,runId:p.runId,events:[]};rows.set(key,row);}
  row.events.push(event);if(first)row.start=event;else row.end=event;
 }
 return [...rows.values()];
}
function kunPretty(value){return typeof value==="string"?value:JSON.stringify(value,null,2)??"未记录";}
function kunRaw(label,value){return el("details",{class:"kun-raw"},el("summary",{},label),el("pre",{},kunPretty(value)));}
function kunFacts(pairs){return el("dl",{class:"kun-facts"},...pairs.flatMap(([k,v])=>[el("dt",{},k),el("dd",{},String(v??"未记录"))]));}
function kunCallName(row){const d=(row.start||row.end)?.data.data||{};return row.kind==="model"?"模型 · 步骤 "+d.step:row.kind==="tool"?"工具 · "+d.call?.function?.name:"MCP · "+d.server+" / "+d.method;}
function kunCallStatus(row){const d=row.end?.data.data;return d?.replay?.mode==="recorded"?"录制回放（未执行）":!d?"结果未记录":d.error||d.isError?"失败":d.status||"已返回";}
function kunRenderCall(row,sequence){
 const visible=kunCallRows(row.events.filter(e=>e.data.sequence<=sequence))[0];
 if(!visible)return el("p",{},"此快照尚无调用证据。");
 const a=visible.start?.data,d=a?.data||{},b=visible.end?.data,r=b?.data||{};
 const node=el("section",{class:"kun-call-evidence"},el("h3",{},kunCallName(visible)),
  kunFacts([["运行",row.runId],["结果",kunCallStatus(visible)],["请求事件",a?"#"+a.sequence:"未保留 / 未派发"],["结果事件",b?"#"+b.sequence:"此快照尚未记录"],["耗时",r.durationMs==null?"未知":r.durationMs+" ms"]]));
 if(row.kind==="model"){
  node.append(kunFacts([["模型",d.request?.model],["输入消息",d.request?.messages?.length],["工具定义",d.request?.tools?.length],["服务报告用量",r.usage==null?"未知":kunPretty(r.usage)]]));
  if(b)node.append(el("h4",{},"模型回答"),el("pre",{},r.message?.content||"（无文本回答）"),kunRaw("模型工具调用",r.message?.tool_calls||[]));
 }else if(row.kind==="tool"){
  node.append(kunFacts([["调用 ID",(d.call||r.call)?.id],["状态",r.status||"结果未记录"]]),kunRaw("工具参数",(d.call||r.call)?.function?.arguments));
  if(r.replay)node.append(el("p",{class:"experiment-mode"},"录制回放 · 未执行真实工具"),kunFacts([["来源序号",r.replay.sourceSequence],["录制位置",r.replay.position+1],["原始结果",r.replay.recordedStatus]]));
  if(b)node.append(el("h4",{},r.replay?"录制输出":"工具输出"),el("pre",{},r.output||"（空输出）"));
 }else{
  node.append(kunFacts([["交换 ID",d.exchangeId??r.exchangeId],["服务器",d.server||r.server],["方法",d.method||r.method]]),kunRaw("MCP 参数",d.params));
  if(b)node.append(el("h4",{},r.error?"MCP 错误":"MCP 结果"),el("pre",{},kunPretty(r.error||r.result)));
 }
 node.append(el("p",{class:"help"},"只展示截至固定快照 #"+sequence+" 的记录。缺少完成记录不能证明成功；工具内部未上报的网络请求不可见。"));
 if(a)node.append(kunRaw("请求原始证据 #"+a.sequence,a));
 if(b)node.append(kunRaw("结果原始证据 #"+b.sequence,b));
 return node;
}
function kunRenderContext(selected,snapshot,current){
 const event=selected?.data,actual=selected?.method==="kun/model.started"&&event?.data?.request;
 const s=selected?snapshot?.state:current;
 const request=actual||{messages:s?.messages,tools:s?.toolDefinitions,model:s?.config?.model};
 const node=el("section",{class:"kun-context",'data-sequence':event?.sequence||0},el("h3",{},actual?"实际模型请求":"状态上下文"),
  el("p",{class:"help"},actual?"来源：模型请求事件 #"+event.sequence+"。按当时发送顺序显示；后续刷新不会改变此请求。":"这里显示所选状态中的消息。选择左侧模型请求，查看实际发送给模型的输入。"));
 if(!s&&!actual){node.append(el("p",{},"当前快照不可用。"));return node;}
 node.append(kunFacts([["运行",event?.runId||s?.runId],["快照",event?"#"+event.sequence:"当前状态"],["状态版本",s?.revision],["模型",request.model],["消息",request.messages?.length||0],["工具定义",request.tools?.length||0]]));
 const messages=el("ol",{class:"kun-context-messages"});
 for(const [index,message] of (request.messages||[]).entries()){
  const content=message.content||"";
  const entry=el("details",{},el("summary",{},"#"+(index+1)+" · "+message.role+" · "+Array.from(content).length+" 字符"+(message.tool_call_id?" · 调用 "+message.tool_call_id:"")),el("pre",{},content||"（无文本内容）"));
  if(message.tool_calls?.length)entry.append(kunRaw("工具调用声明",message.tool_calls));
  messages.append(el("li",{},entry));
 }
 node.append(el("h4",{},"有序消息"),messages,el("p",{class:"help"},"字符数用于检查内容长度，不代表 token 数。"));
 const skills=el("ul",{class:"kun-skill-evidence"});
 for(const skill of s?.skills||[])skills.append(el("li",{},el("strong",{},skill.name),kunFacts([["路径",skill.path],["内容哈希",skill.hash]]),kunRaw("启动时捕获的技能内容",skill.content)));
 node.append(el("h4",{},"技能来源"),el("p",{class:"help"},"下列技能是运行启动时捕获的内容；是否出现在此请求中，以系统消息为准。没有独立的逐次读取轨迹。"),skills,kunRaw("工具定义",request.tools||[]));
 if(actual)node.append(kunRaw("实际请求原始 JSON",actual));
 if(snapshot)node.append(kunRaw("快照原始 JSON",snapshot));
 return node;
}
function kunDiffView({sid,getSelected}){
 let baseline=null,generation=0,busy=false;
 const target=el("p",{class:"help"}),status=el("p",{role:"status"}),result=el("div",{class:"kun-diff-result"});
 const set=button("设为比较起点",()=>{baseline=getSelected()?.data||null;generation++;busy=false;result.replaceChildren();status.textContent="";update();});
 const compare=button("比较到所选快照",async()=>{
  const to=getSelected()?.data,from=baseline;if(!to||!from)return;
  const ticket=++generation;busy=true;compare.disabled=true;status.textContent="正在读取两份固定快照…";
  try{
   const value=await api("/sessions/"+sid+"/debug/query?kind=diff&fromSequence="+from.sequence+"&sequence="+to.sequence);
   if(ticket!==generation)return;
   const diff=value.data;
   result.replaceChildren(el("h4",{},"#"+diff.from.sequence+" → #"+diff.to.sequence),
    kunFacts([["起点运行 / 版本",diff.from.runId+" / "+diff.from.revision],["终点运行 / 版本",diff.to.runId+" / "+diff.to.revision]]));
   status.textContent=diff.truncated?"结果已截断；最多 "+diff.limit+" 项、遍历 "+diff.nodeLimit+" 节点。请缩小比较范围。":diff.changes.length?"共 "+diff.changes.length+" 处差异。":"脱敏后的状态没有差异。";
   for(const change of diff.changes){
    const preview=(name,v)=>el("div",{},el("strong",{},name),el("pre",{},v?v.preview+(v.truncated?"\n…预览已截断（共 "+v.bytes+" JSON 字节）":""):"（不存在）"));
    result.append(el("details",{},el("summary",{},change.change+" · "+change.path+(change.pathTruncated?"…（路径已截断）":"")),el("div",{class:"kun-diff-values"},preview("之前",change.before),preview("之后",change.after))));
   }
   result.append(kunRaw("差异原始 JSON",value));
  }catch(e){if(ticket===generation)status.textContent=e.message;}
  finally{if(ticket===generation){busy=false;update();}}
 });
 const node=el("details",{class:"kun-diff"},el("summary",{},"比较两份快照（只读）"),el("p",{class:"help"},"在调用列表选择快照并设为起点，再选另一快照比较。数组按位置比较；字段先脱敏，单值预览最多 512 字符。差异不包含文件系统变化，也不会回滚或重新执行。"),el("div",{class:"actions"},set,compare),target,status,result);
 function update(){const selected=getSelected()?.data;set.disabled=!selected;compare.disabled=busy||!baseline||!selected;target.textContent="起点："+(baseline?"#"+baseline.sequence+" · "+baseline.runId:"未选择")+"；终点："+(selected?"#"+selected.sequence+" · "+selected.runId:"未选择固定快照");}
 return {node,update};
}

// Stable semantic keys preserve expansion when newer cards/rows are inserted.
function kunDetailKey(node){
 const parts=[];for(let current=node;current;current=current.parentElement){if(current.dataset?.module)parts.unshift("module:"+current.dataset.module);if(current.tagName==="DETAILS")parts.unshift(current.dataset.kunKey||current.querySelector(":scope > summary")?.textContent||"");}
 return JSON.stringify(parts);
}
