// Small shared widgets; queries never become messages in the target conversation.
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
 const query=el("select",{},...Object.entries({run:"运行状态",context:"上下文",tools:"工具目录",budget:"预算",modules:"模块",breakpoints:"断点与控制",actions:"动作账本"}).map(([value,label])=>el("option",{value},label)));
 const target=el("p",{class:"help"}),feedback=el("p",{role:"status"}),history=el("div",{class:"kun-console-history"});
 const append=(title,data)=>{history.prepend(el("details",{open:true},el("summary",{},title),el("pre",{},JSON.stringify(data,null,2))));while(history.children.length>20)history.lastElementChild.remove();};
 const inspect=button("执行只读查询",async()=>{
  const sequence=getSelected()?.data.sequence||0;
  try{const result=await api("/sessions/"+sid+"/kun/query?kind="+query.value+"&sequence="+sequence);
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
  const current=getCurrent();if(!current||!["running","paused","pausing"].includes(current.status))throw Error("当前没有可控制的运行");
  if(operation.value==="steer"&&!text.value.trim())throw Error("请输入补充指令");
  proposal={requestId:crypto.randomUUID(),runId:current.runId,expectedStateRevision:current.revision,operation:operation.value};
  if(operation.value==="steer")proposal.text=text.value;
  preview.textContent=JSON.stringify({target:"当前运行；固定历史快照不作为控制目标",command:proposal},null,2);preview.classList.remove("hidden");apply.disabled=false;
 });
 operation.onchange=()=>{clear();text.disabled=operation.value!=="steer";};text.disabled=true;text.oninput=clear;
 const node=el("section",{class:"kun-console hidden"},el("h3",{},"Console"),target,
  el("p",{class:"help"},"查询只读取当前状态或选定快照，不会向目标对话添加消息。这里不执行脚本，也没有自然语言诊断模型。"),
  el("div",{class:"actions"},query,inspect),el("h4",{},"控制提案"),
  el("p",{class:"help"},"控制始终针对当前运行。提案固定运行编号和状态版本；过期后重新预览。继续和单步不会代替 MCP 审批。"),
  operation,text,el("div",{class:"actions"},propose,apply),preview,feedback,
  el("div",{class:"actions"},button("清空本地显示",()=>history.replaceChildren())),history);
 return {node,update(){const selected=getSelected(),current=getCurrent();target.textContent=selected?"查询目标：固定快照 #"+selected.data.sequence+" · "+selected.data.runId:"查询目标：当前状态";target.textContent+="；控制目标："+(current?.runId||"无在线运行");}};
}
