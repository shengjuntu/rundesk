// Small shared widgets; queries never become messages in the target conversation.
function kunHarnessChangeReason(s,selected){
 if(selected)return "固定历史快照只读；请先选择“跟随现场”。";
 if(!s)return "当前状态不可用，请刷新。";
 if(s.fork||s.diagnostic)return "首批仅支持普通 Kun 运行；分叉和诊断保持固定组合。";
 if(s.status!=="paused"||s.phase!=="before_model"||s.debug?.pause?.phase!=="before_model")return "请先暂停在模型请求前。";
 if(s.pending?.length||s.approval||s.queuedControls?.length)return "请先完成待处理工具、审批和控制。";
 if(Object.values(s.actions||{}).some(v=>["prepared","dispatched","outcome_unknown"].includes(v)))return "动作尚未完成或结果未知，不能切换。";
 if(s.modules?.capability?.phase!=="ready"||!s.harness?.revision)return "工具目录或组合版本尚未就绪。";
 return "";
}
function kunHarnessSwitch({sid,getCurrent,getSelected,refresh,isActive=()=>true}){
 const choice=el("select",{'aria-label':"当前运行的新组合"},el("option",{value:"tool-loop-v1"},"Tool Loop"),el("option",{value:"plan-act-v1"},"Plan-Act"));choice.value="plan-act-v1";
 const reason=el("textarea",{rows:2,maxLength:2048,'aria-label':"组合切换原因",placeholder:"说明本次实验或调试目的"});
 const hint=el("p",{class:"help"}),feedback=el("p",{role:"status"}),details=el("pre",{class:"hidden"});
 let proposal=null,busy=false,generation=0;
 const clear=()=>{generation++;proposal=null;details.textContent="";details.classList.add("hidden");};
 const preview=button("预览组合切换",async()=>{
  if(busy||kunHarnessChangeReason(getCurrent(),getSelected()))return;
  clear();const token=generation;busy=true;update();
  try{
   await refresh();if(!isActive()||token!==generation)return;
   const s=getCurrent(),blocked=kunHarnessChangeReason(s,getSelected());if(blocked)throw Error(blocked);
   if(choice.value===s.harness.id)throw Error("当前已经使用所选组合。");
   if(!reason.value.trim()||[...reason.value].length>2048)throw Error("请填写 1–2048 字符的切换原因。");
   proposal={requestId:crypto.randomUUID(),runId:s.runId,expectedStateRevision:s.revision,operation:"set_harness",harness:{loopPolicy:choice.value},reason:reason.value};
   details.textContent=JSON.stringify({当前组合:s.harness.id,目标组合:choice.value,当前Harness版本:s.harness.revision,新Harness版本:s.harness.revision+1,状态迁移:"清除当前计划和上下文构建状态；保留历史、预算、工具目录及权限。",后续:choice.value==="plan-act-v1"?"保持暂停；继续后新增一次规划调用，消耗剩余预算。":"保持暂停；继续后直接执行，不再注入旧计划。",command:proposal},null,2);
   details.classList.remove("hidden");feedback.textContent="预览完成，尚未切换。";
  }catch(e){if(isActive()&&token===generation)feedback.textContent=e.message;}
  finally{busy=false;update();}
 });
 const apply=button("应用已预览的组合",async()=>{
  if(busy||!proposal)return;update();if(!proposal||apply.disabled)return;
  const command=proposal,token=generation;busy=true;update();
  try{
   const receipt=await api("/sessions/"+sid+"/kun/control",{method:"POST",body:command});
   if(!isActive()||token!==generation)return;
   clear();feedback.textContent="组合已切换，仍保持暂停。回执："+receipt.status+" · 状态版本 "+receipt.revision;await refresh();
  }catch(e){if(isActive()&&token===generation)feedback.textContent=e.message+"；可用同一提案重试，状态已变化时请查看控制回执。";}
  finally{busy=false;update();}
 });
 function update(){
  const s=getCurrent(),blocked=kunHarnessChangeReason(s,getSelected());
  if(proposal&&(blocked||s.runId!==proposal.runId||s.revision!==proposal.expectedStateRevision)){clear();feedback.textContent="状态或检查目标已变化，旧提案失效；请重新预览或查看已有回执。";}
  hint.textContent=blocked||"控制当前运行 "+s.runId+" · 状态版本 "+s.revision+"；仅管理员可应用。";
  choice.disabled=reason.disabled=busy;preview.disabled=busy||!!blocked;apply.disabled=busy||!!blocked||!proposal;
 }
 choice.onchange=reason.oninput=()=>{clear();update();};
 const node=el("section",{class:"kun-harness-switch"},el("h3",{},"切换当前运行组合"),
  el("p",{class:"help"},"仅在模型请求前安全暂停时切换两套内置组合。清除当前计划，保留已用预算、历史和权限；不改变默认配置，也不自动继续。Plan-Act 切入后需要新增规划调用。"),hint,
  el("label",{},"目标组合",choice),el("label",{},"切换原因",reason),el("div",{class:"actions"},preview,apply),details,feedback);
 update();return {node,update};
}

function kunPolicyEditor(initial={}){
 const rows=el("div",{class:"kun-rule-list"});let entries=[];
 const timeout=el("input",{type:"number",min:0,max:86400,step:1,value:0});
 const add=button("添加断点",()=>addRule({id:"bp-"+crypto.randomUUID().slice(0,12),phase:"before_tool"}));
 const phases={before_model:"模型请求前",after_model:"完整模型响应后",before_tool:"工具执行前",after_tool:"工具结果已记录"};
 function addRule(rule){
  if(entries.length>=16)throw Error("最多 16 条断点规则");
  const phase=el("select",{},...Object.entries(phases).map(([value,label])=>el("option",{value},label)));phase.value=rule.phase;
  const tool=el("input",{value:rule.tool||"",maxLength:256,placeholder:"留空匹配全部工具；MCP 填工具别名"});
  const model=el("input",{value:rule.model||"",maxLength:160,placeholder:"留空匹配当前模型"});
  const once=el("input",{type:"checkbox"});once.checked=!!rule.once;
  const numeric={};const fields=[];
  for(const [key,label,max]of [["minStep","模型步数至少",100],["minToolCalls","已派发工具数至少",3200],["minFailures","连续工具失败数至少",100],["minReportedTokens","已报告 token 至少",1000000000]]){
   const input=el("input",{type:"number",min:0,max,step:1,value:rule[key]||0});numeric[key]=input;fields.push(el("label",{},label,input));
  }
  const entry={rule,phase,tool,model,once,numeric};
  const remove=button("删除",()=>{entries=entries.filter(x=>x!==entry);entry.node.remove();add.disabled=false;});
  entry.node=el("section",{class:"kun-rule"},el("div",{class:"actions"},el("strong",{},rule.id),remove),
   el("label",{},"停止位置",phase),el("label",{class:"kun-check"},once,"每个规则版本只命中一次"),
   el("details",{},el("summary",{},"筛选条件（同时满足；0 表示不限制）"),el("div",{class:"kun-rule-fields"},el("label",{},"工具名 / MCP 别名",tool),el("label",{},"模型名称",model),...fields)));
  const sync=()=>{tool.disabled=!phase.value.endsWith("tool");if(tool.disabled)tool.value="";};phase.onchange=sync;sync();
  entries.push(entry);rows.append(entry.node);add.disabled=entries.length>=16;
 }
 const node=el("section",{class:"kun-policy-editor"},el("h3",{},"条件断点"),
  el("p",{class:"help"},"默认关闭。任一规则命中即暂停；同一规则内所有条件须满足。工具名使用实际调用名，MCP 别名可在 Console 工具目录中查询。"),rows,add,
  el("label",{},"调试暂停超时（秒，0 一直等待；超时停止运行）",timeout),
  el("p",{class:"help"},"超时只适用于调试暂停，独立于 MCP 审批等待。已有暂停的期限不会因编辑规则而延长。"));
 const set=policy=>{entries=[];rows.replaceChildren();timeout.value=policy?.pauseTimeoutSeconds||0;add.disabled=false;for(const rule of policy?.breakpoints||[])addRule(rule);};
 const get=()=>{
  for(const input of node.querySelectorAll("input")){if(!input.checkValidity())throw Error("请检查断点条件中的数值范围");}
  return {pauseTimeoutSeconds:Number(timeout.value),breakpoints:entries.map(x=>{
   const rule={id:x.rule.id,phase:x.phase.value,once:x.once.checked};
   if(x.tool.value.trim())rule.tool=x.tool.value.trim();if(x.model.value.trim())rule.model=x.model.value.trim();
   for(const [key,input]of Object.entries(x.numeric)){const value=Number(input.value);if(value)rule[key]=value;}
   return rule;
  })};
 };
 set(initial);return {node,set,get};
}

function kunConsole({sid,getCurrent,getSelected,refresh}){
 const query=el("select",{},...Object.entries({run:"运行状态",context:"上下文",tools:"工具目录",budget:"预算",modules:"模块",breakpoints:"断点与控制",actions:"动作账本",evidence:"选定事件证据"}).map(([value,label])=>el("option",{value},label)));
 const target=el("p",{class:"help"}),feedback=el("p",{role:"status"}),history=el("div",{class:"kun-console-history"});
 const append=(title,data)=>{history.prepend(el("details",{open:true},el("summary",{},title),el("pre",{},JSON.stringify(data,null,2))));while(history.children.length>20)history.lastElementChild.remove();};
 const inspect=button("执行只读查询",async()=>{
  const sequence=getSelected()?.data.sequence||0;
  try{const result=await api("/sessions/"+sid+"/debug/query?kind="+query.value+"&sequence="+sequence);
   append("查询 · "+query.selectedOptions[0].textContent+" · "+result.runId+" · 版本 "+result.revision+(sequence?" · 快照 #"+sequence:""),result);feedback.textContent="查询完成，没有调用模型或工具。";
  }catch(e){feedback.textContent=e.message;}
 });
 const operation=el("select",{},...Object.entries({pause:"暂停",resume:"继续",step:"单步",cancel:"停止",steer:"补充指令"}).map(([value,label])=>el("option",{value},label)));
 const text=el("textarea",{rows:2,maxLength:262144,placeholder:"仅“补充指令”会将此文本写入当前运行"});
 const preview=el("pre",{class:"kun-proposal hidden"});let proposal=null;
 const clear=()=>{proposal=null;preview.textContent="";preview.classList.add("hidden");apply.disabled=true;};
 const apply=button("执行已预览的控制",async()=>{
  if(!proposal)return;apply.disabled=true;
  const command=proposal;
  try{const receipt=await api("/sessions/"+sid+"/kun/control",{method:"POST",body:command});append("控制回执 · "+command.operation,{command,receipt});clear();feedback.textContent="控制回执："+receipt.status;await refresh();}
  catch(e){feedback.textContent=e.message;apply.disabled=false;}
 });apply.disabled=true;
 const propose=button("预览控制提案",()=>{
  const current=getCurrent();const reason=kunControlReason(current,operation.value);if(reason)throw Error(reason);
  if(operation.value==="steer"&&!text.value.trim())throw Error("请输入补充指令");
  proposal={requestId:crypto.randomUUID(),runId:current.runId,expectedStateRevision:current.revision,operation:operation.value};
  if(operation.value==="steer")proposal.text=text.value;
  preview.textContent=JSON.stringify({target:"当前运行；固定历史快照不作为控制目标",command:proposal},null,2);preview.classList.remove("hidden");apply.disabled=false;
 });
 operation.onchange=()=>{clear();text.disabled=operation.value!=="steer";};text.disabled=true;text.oninput=clear;
 const node=el("section",{class:"kun-console hidden"},el("h3",{},"Console"),target,
  el("p",{class:"help"},"查询只读取当前状态或选定快照，不会向目标对话添加消息。自然语言问题可另建独立诊断会话；会调用模型，用量单独记录。"),
  el("div",{class:"actions"},query,inspect,button("独立诊断",async()=>{const source=await api("/sessions/"+encodeURIComponent(sid)),selected=getSelected();RunDeskDebug.open(source,{runId:selected?.data.runId||getCurrent()?.runId,...(selected?.id?{through:selected.id}:{})});})),el("h4",{},"控制提案"),
  el("p",{class:"help"},"控制始终针对当前运行。提案固定运行编号和状态版本；过期后重新预览。继续和单步不会代替 MCP 审批。"),
  operation,text,el("div",{class:"actions"},propose,apply),preview,feedback,
  el("div",{class:"actions"},button("清空本地显示",()=>history.replaceChildren())),history);
 return {node,update(){const selected=getSelected(),current=getCurrent();target.textContent=selected?"查询目标：固定快照 #"+selected.data.sequence+" · "+selected.data.runId:"查询目标：当前状态";target.textContent+="；控制目标："+(current?.runId||"无在线运行");}};
}
