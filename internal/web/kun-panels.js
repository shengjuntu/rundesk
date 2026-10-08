// Structured projections of recorded state. No network or control side effects.
const kunStateLabels={replayed:"录制回放（未执行）",recorded:"固定录制",running:"运行中",paused:"已暂停",pausing:"等待暂停",completing:"正在结束",completed:"已完成",failed:"失败",interrupted:"已中断",idle:"空闲",prepared:"待派发",dispatched:"已派发",succeeded:"成功",rejected:"参数拒绝",declined:"审批拒绝",cancelled:"已取消",outcome_unknown:"结果未知",pending:"待初始化",ready:"就绪",connecting:"连接中",closed:"已关闭"};
function kunStateLabel(value){return kunStateLabels[value]||value||"未记录";}
function kunNumber(value){return typeof value==="number"&&Number.isFinite(value)&&value>=0?value:null;}
function kunMillis(value){return kunNumber(value)===null?"未记录":(value/1000).toFixed(3)+" s";}
function kunScopedEvents(events,s,selected){
 const runId=selected?.data.runId||s?.runId;if(!runId)return [];
 return events.filter(e=>e.data?.runId===runId&&(!selected||e.data.sequence<=selected.data.sequence)&&(!s||e.data.revision<=s.revision));
}
function kunPanel(title,name,s,selected){
 return el("section",{class:"kun-panel kun-"+name,'data-sequence':selected?.data.sequence||0,'data-run-id':s?.runId||selected?.data.runId||"",'data-revision':s?.revision??"",'data-status':s?.status||"",'data-phase':s?.phase||""},
  el("h3",{},title),kunFacts([["运行",s?.runId||selected?.data.runId],["检查时点",selected?"固定快照 #"+selected.data.sequence:"当前状态"],["状态版本",s?.revision]]));
}
function kunTable(headings,rows){
 if(!rows.length)return el("p",{class:"help"},"此范围内没有已加载记录。");
 return el("div",{class:"kun-table-wrap"},el("table",{class:"kun-table"},el("thead",{},el("tr",{},...headings.map(h=>el("th",{scope:"col"},h)))),el("tbody",{},...rows.map(cells=>el("tr",{},...cells.map(c=>el("td",{},c??"未记录")))))));
}
function kunEvidenceLink(event,navigate,label){return event?button(label||"证据 #"+event.data.sequence,()=>navigate(event),"quiet"):el("span",{class:"help"},"当前已加载事件中无对应证据");}
function kunRenderEvent(event){
 const p=event.data,d=p.data||{};
 const node=kunPanel("事件证据","event-evidence",{runId:p.runId,revision:p.revision},event);
 node.append(kunFacts([["类型",event.method],["时间",p.time]]));
 if(d.command)node.append(kunFacts([["操作",d.command.operation],["请求 ID",d.command.requestId],["回执",d.receipt?.status],["回执版本",d.receipt?.revision]]));
 if(event.method==="kun/approval.requested")node.append(kunApprovalView(d));
 node.append(kunRaw("事件原始 JSON",p));return node;
}
function kunApprovalView(a){
 if(!a)return el("p",{class:"help"},"此状态没有待处理审批。");
 return el("section",{class:"kun-approval-state"},kunFacts([["调用 ID",a.callId],["服务器 / 工具",a.server+" / "+a.tool],["决定",a.decision?({approve:"已允许，等待引擎处理",reject:"已拒绝，等待引擎处理"}[a.decision]||a.decision):"等待明确允许或拒绝"]]),kunRaw("本次调用参数",a.arguments));
}
function kunControlReason(s,operation){
 if(!s)return "worker 当前离线，无法控制";
 if(!["running","paused","pausing"].includes(s.status))return "当前运行已结束或尚未开始";
 if(operation==="resume"||operation==="step"){
  if(s.approval)return "MCP 审批尚未处理完，继续/单步不能代替审批";
  if(s.status!=="paused")return "需先在安全边界暂停";
 }
 if(operation==="approve"||operation==="reject"){
  if(!s.approval)return "当前没有待处理 MCP 审批";
  if(s.approval.decision)return "本次审批已有决定";
 }
 if(operation==="pause"&&(s.status==="paused"||s.queuedControls?.some(c=>c.operation==="pause")))return "已经暂停或已提交暂停请求";
 if(operation==="cancel"&&s.queuedControls?.some(c=>c.operation==="cancel"))return "停止请求已提交";
 return "";
}
function kunRenderSources(s,selected,events,navigate){
 const node=kunPanel("当前运行控制状态","sources-state",s,null);
 node.append(el("p",{class:"help"},"控制目标始终是当前运行。"+(selected?"历史选择仍固定在 #"+selected.data.sequence+"，不会成为控制目标。":"")));
 if(!s){node.append(el("p",{},"当前状态不可用，可检查恢复条件或查看保留事件。"));return node;}
 if(s.fork)node.append(el("p",{class:"experiment-mode"},"Hybrid · 工具仅录制回放 · 未命中停止"),kunFacts([["来源边界",s.fork.origin.sequence],["录制进度",s.fork.replayCursor+" / "+s.fork.replayTotal],["新增模型调用",s.step-s.fork.inheritedStep]]));
 const scoped=kunScopedEvents(events,s,null),pause=s.debug?.pause;
 node.append(kunFacts([["状态",kunStateLabel(s.status)],["阶段",s.phase],["模型步骤",s.step],["待派发工具",s.pending?.length??0],["断点规则版本",s.debug?.revision],["预算停止原因",s.budget?.stopReason||"无已记录原因"]]));
 if(s.error)node.append(el("p",{class:"kun-signal"},"运行错误："+s.error));
 if(pause)node.append(el("h4",{},"调试暂停"),kunFacts([["原因",pause.reason],["边界",pause.phase],["命中规则",pause.ruleIds?.join(", ")||"无条件规则 ID"],["工具调用",pause.callId||"无"],["暂停期限",pause.deadline||"无限等待"]]));
 node.append(el("h4",{},"当前 MCP 审批"),kunApprovalView(s.approval));
 if(s.resumedFrom)node.append(el("h4",{},"续跑来源"),kunFacts([["原运行",s.resumedFrom.sourceRunId],["安全检查点",s.resumedFrom.sequence]]));
 const rules=s.debug?.policy?.breakpoints||[];
 node.append(el("h4",{},"当前断点"),kunFacts([["模型前固定暂停",s.config?.pauseBeforeModel?"开启":"关闭"],["调试暂停超时",s.debug?.policy?.pauseTimeoutSeconds? s.debug.policy.pauseTimeoutSeconds+" s":"无限等待"]]),
  kunTable(["规则","阶段 / 条件","命中","次数限制"],rules.map(rule=>[rule.id,rule.phase+" · "+Object.entries(rule).filter(([k,v])=>!["id","phase","once"].includes(k)&&v).map(([k,v])=>k+"="+v).join(", "),s.debug?.hits?.[rule.id]??0,rule.once?"只命中一次":"每次匹配"])),
  el("h4",{},"待应用控制"),kunTable(["操作","请求 ID","内容"],(s.queuedControls||[]).map(c=>[c.operation,c.requestId,c.text?kunRaw("查看补充指令 "+c.requestId,c.text):"—"])));
 const recent=scoped.filter(e=>e.method.startsWith("kun/control.")).slice(-50).reverse();
 node.append(el("h4",{},"最近控制记录（最多 50 条）"),kunTable(["操作","回执","请求 ID","证据"],recent.map(e=>{const d=e.data.data||{};return [d.command?.operation,d.receipt?.status||e.method.split(".").pop(),d.command?.requestId,kunEvidenceLink(e,navigate)];})));
 const actions=Object.entries(s.actions||{}),totals={};for(const [,status]of actions)totals[status]=(totals[status]||0)+1;
 node.append(el("h4",{},"动作账本"),kunFacts(Object.entries(totals).map(([k,v])=>[kunStateLabel(k),v])),el("p",{class:"help"},"显示最多 200 项，共 "+actions.length+" 项；待派发不表示已经执行。"),
  kunTable(["调用 ID","状态","调用证据"],actions.slice(0,200).map(([id,status])=>[id,kunStateLabel(status),kunEvidenceLink(scoped.findLast(e=>e.data.data?.call?.id===id&&/^kun\/tool\.(started|completed)$/.test(e.method)),navigate)])),
  kunRaw("控制状态原始 JSON",{runId:s.runId,revision:s.revision,status:s.status,phase:s.phase,debug:s.debug,approval:s.approval,queuedControls:s.queuedControls,actions:s.actions,resumedFrom:s.resumedFrom}));
 return node;
}
function kunMetric(label,value,limit,format=String,disabledAtZero=false){
 const used=kunNumber(value),cap=kunNumber(limit),disabled=disabledAtZero&&cap===0;
 const node=el("section",{class:"kun-metric"},el("h4",{},label),el("strong",{},used===null?"未记录":format(used)),el("p",{class:"help"},disabled?"阈值关闭":cap===null?"上限未记录":"上限 "+format(cap)));
 if(used!==null&&cap>0){node.append(el("progress",{max:cap,value:Math.min(used,cap),'aria-label':label+"用量"}));if(used>=cap)node.append(el("span",{class:"kun-signal"},"已到达阈值"));}
 return node;
}
function kunReportedUsage(usage){
 if(!usage||typeof usage!=="object")return "未报告";
 for(const key of ["total_tokens","prompt_tokens","completion_tokens"]){if(usage[key]!=null&&!Number.isSafeInteger(usage[key]))return "未报告有效总量";}
 const valid=v=>Number.isInteger(v)&&v>=0&&v<=2000000000;
 if(valid(usage.total_tokens))return String(usage.total_tokens);
 if(usage.total_tokens==null&&valid(usage.prompt_tokens)&&valid(usage.completion_tokens)&&usage.prompt_tokens<=1000000000&&usage.completion_tokens<=1000000000)return String(usage.prompt_tokens+usage.completion_tokens)+"（输入+输出）";
 return "未报告有效总量";
}
function kunRenderPerformance(s,selected,events,navigate){
 const node=kunPanel("运行用量与耗时","performance",s,selected),hasBudget=!!s?.harness?.id&&!!s?.budget,budget=hasBudget?s.budget:{},limits=hasBudget?s.config?.budget||{}:{};
 if(s&&!hasBudget)node.append(el("p",{class:"help"},"此记录缺少预算模块标识，累计预算按未知显示，兼容旧快照。"));
 if(!s)node.append(el("p",{class:"help"},"此时点的状态不可用；下面仅展示已加载事件，无法给出累计预算。"));
 node.append(el("div",{class:"kun-metrics"},kunMetric("已发起模型调用",s?.step,s?.config?.maxSteps),kunMetric(s?.fork?"工具预算（继承 + 回放）":"已派发工具",budget.toolCalls,limits.maxToolCalls),
  kunMetric("已报告 token",budget.reportedTokens,limits.maxTotalTokens,String,true),kunMetric("活动时间",budget.activeMillis,kunNumber(limits.maxActiveSeconds)===null?null:limits.maxActiveSeconds*1000,kunMillis),
  kunMetric("连续工具失败",budget.consecutiveFailures,limits.maxConsecutiveFailures)),
  kunFacts([["人工等待时间",kunMillis(budget.waitMillis)],["未报告有效用量的已完成模型调用",budget.unreportedModelCalls],["预算停止原因",budget.stopReason||"无已记录原因"],["费用", "未估算"]]),
  el("p",{class:"help"},"累计值来自上述状态版本；当前状态中的活动/等待计时不保证实时刷新。token 仅包含有效报告，缺失部分未知；阈值不保证当前请求不超额。调用耗时彼此可能嵌套，不能相加当作总运行时间。"));
 if(s?.fork)node.append(el("h4",{},"分支新增用量"),kunFacts([["继承模型调用",s.fork.inheritedStep],["新增模型调用",s.step-s.fork.inheritedStep],["继承已报告 token",s.fork.inheritedBudget.reportedTokens],["新增已报告 token",budget.reportedTokens-s.fork.inheritedBudget.reportedTokens],["回放工具结果",s.fork.replayCursor],["本分支真实工具调用",0]]),el("p",{class:"help"},"上述预算总量包含来源检查点的用量；回放消耗工具预算，但没有外部工具调用。"));
 const scoped=kunScopedEvents(events,s,selected);
 for(const [kind,label,method]of [["model","模型","kun/model.completed"],["tool","工具","kun/tool.completed"],["mcp","MCP 交换（可能嵌套于工具调用）","kun/mcp.response"]]){
  const all=scoped.filter(e=>e.method===method),rows=all.slice(-200).reverse();
  node.append(el("h4",{},label),el("p",{class:"help"},"已加载 "+all.length+" 条完成记录，展示最近 "+rows.length+" 条；不代表服务端完整历史。"),
   kunTable(["步骤 / 调用","结果","耗时","已报告 token","证据"],rows.map(e=>{const d=e.data.data||{};return [kind==="model"?"模型 "+d.step:kind==="tool"?d.call?.function?.name:d.server+" / "+d.method,d.status?kunStateLabel(d.status):d.error||d.isError?"失败":"已返回",kunMillis(d.durationMs),kind==="model"?kunReportedUsage(d.usage):"—",kunEvidenceLink(e,navigate)];})));
 }
 node.append(kunRaw("用量与限制原始 JSON",{budget:s?.budget,limits:s?.config?.budget,modelCalls:s?.step,maxModelCalls:s?.config?.maxSteps}));return node;
}
function kunRenderApplication(s,selected,events,navigate,showSources){
 const node=kunPanel("MCP 服务与审批","application",s,selected);
 if(!s){node.append(el("p",{},"此时点的状态不可用；可在 Network 查看保留事件。"));return node;}
 const scoped=kunScopedEvents(events,s,selected);
 if(s.fork)node.append(el("p",{class:"experiment-mode"},"Hybrid 固定目录 · 没有连接真实 MCP；下列审批配置仅为来源记录，回放不会请求或复用真实执行审批。"));
 node.append(kunFacts([["本轮交互策略",s.approvalPolicy==="never"?"禁止询问：需要逐次审批的调用将拒绝":s.approvalPolicy||"允许逐次询问"]]),
  el("p",{class:"help"},"这是所选运行已捕获的目录。修改配置后下一轮才生效。内置文件写权限与 MCP 权限独立；此处只显示本轮目录工具，未列出的工具不等于已允许。"));
 const servers=s.mcp||[];
 node.append(el("h4",{},"服务状态"),kunTable(["服务 / 配置版本","传输 / 协议","状态","目录工具数","最近事件"],servers.map(server=>[
  server.name+" / "+server.configRevision,server.transport+" / "+(server.protocolVersion||"未记录"),el("span",{},kunStateLabel(server.status),server.error?el("p",{class:"kun-signal"},server.error):null),server.toolCount,
  kunEvidenceLink(scoped.findLast(e=>e.method.startsWith("kun/mcp.")&&(e.data.data?.server===server.name||e.data.data?.name===server.name)),navigate)])));
 node.append(el("h4",{},"此时点审批"),kunApprovalView(s.approval));
 if(s.approval)node.append(button("查看当前控制",showSources),el("p",{class:"help"},"处理审批前请在 Sources 核对当前运行和调用 ID；历史审批不能直接提交。"));
 const tools=s.mcpTools||[],toolList=el("div",{class:"kun-mcp-tools"});
 for(const tool of tools.slice(0,200)){
  const mode=tool.approvalMode==="approve"?"始终允许（仍需参数与预算校验）":tool.approvalMode==="prompt"?(s.approvalPolicy==="never"?"需逐次询问，但本轮禁止询问，因此拒绝":"每次调用前询问"):"未知模式："+(tool.approvalMode||"未记录");
  toolList.append(el("details",{'data-kun-key':"tool:"+tool.alias},el("summary",{},tool.server+" / "+tool.name+" · "+mode),
   kunFacts([["模型可见别名",tool.alias],["审批模式",tool.approvalMode]]),el("p",{},tool.description||"无描述"),kunRaw("参数 Schema",tool.inputSchema),kunRaw("工具原始定义",tool)));
 }
 node.append(el("h4",{},"本轮目录工具"),el("p",{class:"help"},"共 "+tools.length+" 项，展开列表最多 200 项；完整目录见原始 JSON。"),toolList,
  kunRaw("MCP 状态原始 JSON",{mcp:s.mcp,mcpTools:s.mcpTools,approvalPolicy:s.approvalPolicy,approval:s.approval}));return node;
}
function kunModuleEvidence(name,module,events){
 if(!module||module.phase==="pending")return null;
 return events.findLast(e=>{
  if(name==="memory"||name==="planning")return e.method==="kun/context.built";
  if(name==="capability")return e.method==="kun/capability.ready";
  if(name==="action")return e.method===(module.phase==="validated"?"kun/tool.validated":"kun/tool.completed")&&e.data.data?.call?.id===module.data?.callId;
  return false;
 });
}
function kunRenderLayers(s,selected,events,navigate){
 const node=kunPanel("四模块记录","layers",s,selected);
 if(!s){node.append(el("p",{},"此时点的模块状态不可用。"));return node;}
 const h=s.harness||{},scoped=kunScopedEvents(events,s,selected);
 node.append(kunFacts([["LoopPolicy",h.id],["版本",h.version],["Harness 版本",h.revision]]),el("p",{class:"help"},"下列是模块最后记录的状态，不是实时健康评分。当前实现固定，无法在运行中替换；Planning 没有单独的规划模型调用。"));
 const cards=el("div",{class:"kun-module-grid"});
 for(const [name,title]of [["memory","Memory · 上下文"],["planning","Planning · 规划"],["action","Action · 动作"],["capability","Capability · 工具目录"]]){
  const m=s.modules?.[name],v=m?.implementation||h.modules?.[name]||{},d=m?.data||{};
  const card=el("section",{class:"kun-module-card",'data-module':name},el("h4",{},title),kunFacts([["实现",v.id],["版本 / 状态结构",(v.version||"未记录")+" / "+(v.stateSchemaVersion??"未记录")],["记录阶段",m?.phase||"未记录"]]));
  let facts=[];
  if(name==="memory")facts=[["消息数",d.messageCount],["请求 JSON 字节数",d.requestBytes],["压缩",d.compression],["截断",d.truncated==null?"未记录":d.truncated?"是":"否"]];
  if(name==="planning")facts=[["规划状态",d.status==="not_requested"?"未请求独立规划":d.status],["说明",d.reason]];
  if(name==="action")facts=[["调用 ID",d.callId],["工具",d.tool],["结果",kunStateLabel(d.status)],["参数 Schema 哈希",d.schemaHash]];
  if(name==="capability")facts=[["工具数",d.toolCount],["Schema 方言",d.schemaDialect],["format 语义",d.format]];
  card.append(kunFacts(facts),kunEvidenceLink(kunModuleEvidence(name,m,scoped),navigate),kunRaw("模块原始状态",m||null));cards.append(card);
 }
 node.append(cards,kunRaw("Harness 原始定义",h));return node;
}
