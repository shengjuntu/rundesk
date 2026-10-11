// Rules and reports are immutable host objects. Nothing here starts a run.
window.RunDeskKunEvaluation=(()=>{
 const enc=encodeURIComponent,kinds={reply_contains:'最终回复包含',reply_not_contains:'最终回复不包含',reply_equals:'最终回复完全等于'},states={pass:'通过',fail:'未通过',unknown:'未知'};
 const reasons={incomplete_records:'记录不完整',completed:'已成功结束',terminal_not_completed:'结束状态不是成功',terminal_status_missing:'缺少可识别的结束状态',reply_exceeds_256_kib:'完整回复超过评分上限',no_final_reply:'没有新增最终回复',matched:'满足规则',not_matched:'不满足规则'};
 const score=v=>v==null?'未知':v.toFixed(2)+' 分';
 function ruleList(r){return el('div',{},el('p',{},'固定项：运行成功结束'),...r.rules.map((rule,i)=>el('div',{},el('strong',{},`${i+1}. ${kinds[rule.kind]}`),el('pre',{},rule.value))));}
 function render(report,openEvidence){
  const out=el('section',{class:'kun-comparison-result'},el('h4',{},'已保存评分 · '+report.rubric.title),kunFacts([['报告时间',report.createdAt],['报告 ID',report.id],['规则指纹',report.rubric.hash],['固定对照指纹',report.comparison.hash],['得分差（右 − 左）',report.scoreDelta==null?'不可计算':report.scoreDelta.toFixed(2)]]));
  for(const warning of report.warnings)out.append(el('p',{class:'help'},warning));
  const columns=el('div',{class:'kun-comparison-columns'});
  for(const [name,value,side]of [['左侧',report.left,report.comparison.left],['右侧',report.right,report.comparison.right]]){
   const col=el('section',{},el('h4',{},name+' · '+states[value.status]+' · '+score(value.score)),kunFacts([['会话',side.sessionId],['模式',side.mode],['固定宿主上界',side.through],['通过/失败/未知',`${value.passed} / ${value.failed} / ${value.unknown}`],['回复状态',({available:'已读取完整回复',missing:'没有最终回复',incomplete:'记录不足',too_large:'回复超过 256 KiB'})[value.reply.status]||'未知'],['回复指纹',value.reply.hash||'无']]),el('p',{class:'help'},side.hypothesis?'本侧包含人工假设；评分不证明假设为真。':'评分仅覆盖所选记录与显式规则。'));
   for(const result of value.results){const label=result.index===0?'运行成功结束':`${result.index}. ${kinds[result.kind]}`;col.append(el('p',{},label+'：'+states[result.status]+'（'+(reasons[result.reason]||result.reason)+'）'));if(result.eventId)col.append(button('查看'+name+'第 '+result.index+' 项证据 #'+result.eventId,()=>openEvidence(side,result.eventId)));}
   columns.append(col);
  }
  out.append(columns,el('details',{},el('summary',{},'固定规则全文'),ruleList(report.rubric)),el('p',{class:'help'},'已保存报告不会随新事件或日志删除改变。证据链接仍依赖原事件留存。'));
  return out;
 }
 function create({preview,isActive=()=>true}){
  let comparison=null,current=null,generation=0,busy=false,rubricOffset=0,reportOffset=0,rubricMore=true,reportMore=true;
  const node=el('section',{class:'kun-comparison'},el('h3',{},'固定规则评分')),info=el('p',{role:'status'}),rules=el('div',{}),result=el('div',{}),history=el('div',{}),fixedRule=el('div',{});
  const title=el('input',{'aria-label':'评分规则标题',value:'回复验收规则',maxLength:120}),choice=el('select',{'aria-label':'已保存评分规则'},el('option',{value:''},'选择固定规则'));choice.value='';
  const rows=[],knownRules=new Set(),knownReports=new Set();
  const loadRules=button('读取评分规则',()=>load('rubrics')),loadReports=button('读取已保存评分',()=>load('evaluations')),save=button('保存固定规则',saveRules),evaluate=button('按当前固定范围评分并保存',run),add=button('增加文本规则',()=>addRow());
  function update(){save.disabled=busy;evaluate.disabled=busy||!current||!comparison;loadRules.disabled=busy||!rubricMore;loadReports.disabled=busy||!reportMore;choice.disabled=busy;title.disabled=busy;add.disabled=busy||rows.length>=12;for(const r of rows){r.kind.disabled=busy;r.value.disabled=busy;r.remove.disabled=busy||rows.length===1;}}
  function edited(){generation++;current=null;choice.value='';fixedRule.replaceChildren();result.replaceChildren();info.textContent='规则输入已变化，请保存为新的固定规则。';update();}
  function addRow(){if(busy||rows.length>=12)return;const kind=el('select',{'aria-label':'文本规则类型'},...Object.entries(kinds).map(([v,text])=>el('option',{value:v},text)));kind.value='reply_contains';const value=el('textarea',{'aria-label':'文本规则内容',rows:2,maxLength:2048,placeholder:'逐字匹配，区分空白和大小写'});const row={kind,value},remove=button('移除此规则',()=>{if(busy||rows.length===1)return;rows.splice(rows.indexOf(row),1);rules.replaceChildren(...rows.map(r=>r.node));edited();});row.remove=remove;row.node=el('div',{class:'kun-rule-row'},kind,value,remove);rows.push(row);rules.append(row.node);kind.addEventListener('change',edited);value.addEventListener('input',edited);edited();}
  title.addEventListener('input',edited);
  function acceptRubric(r){if(!r||!r.id||!r.hash||r.evaluator!=='reply-rules-v1'||!Array.isArray(r.rules)||!r.baselineHash||(comparison&&r.baselineHash!==comparison.baseline.hash))throw Error('规则与当前基线不匹配，请重新读取。');current=r;fixedRule.replaceChildren(el('h4',{},'当前固定规则 · '+r.title),ruleList(r),el('p',{class:'help'},'固定指纹 '+r.hash));if(!knownRules.has(r.id)){knownRules.add(r.id);choice.append(el('option',{value:r.id},r.title));}choice.value=r.id;result.replaceChildren();}
  async function saveRules(){
   if(busy||!isActive())return;
   const body={previewId:preview.id,expectedPreviewHash:preview.hash,title:title.value,rules:rows.map(r=>({kind:r.kind.value,value:r.value.value}))};
   if(!body.title.trim()||body.rules.some(r=>!r.value.trim())){info.textContent='请填写标题和每条规则文本。';return;}
   const t=++generation;busy=true;update();info.textContent='保存固定规则，不启动执行…';
   try{const r=await api('/kun-rubrics',{method:'POST',body});if(!isActive()||t!==generation)return;acceptRubric(r);info.textContent='规则已保存；读取固定对照后可评分。';}
   catch(e){if(isActive()&&t===generation)info.textContent=e.message;}
   finally{if(isActive()&&t===generation){busy=false;update();}}
  }
  choice.addEventListener('change',async()=>{
   const id=choice.value,t=++generation;current=null;fixedRule.replaceChildren();result.replaceChildren();if(!id){update();return;}busy=true;update();
   try{const r=await api('/kun-rubrics/'+enc(id));if(!isActive()||t!==generation)return;if(r.id!==id)throw Error('规则响应身份不匹配。');acceptRubric(r);info.textContent='已选中固定规则。编辑上方输入会取消此选择。';}
   catch(e){if(isActive()&&t===generation)info.textContent=e.message;}
   finally{if(isActive()&&t===generation){busy=false;update();}}
  });
  async function evidence(side,eventId,t){if(!isActive()||t!==generation)return;try{const session=await api('/sessions/'+enc(side.sessionId));if(isActive()&&t===generation)window.RunDeskDebug.open(session,{runId:side.runId,through:side.through,eventId});}catch(e){if(isActive()&&t===generation)info.textContent=e.message;}}
  function show(report,t){if(report.previewId!==preview.id||report.evaluator!=='reply-rules-v1')throw Error('评分报告身份不匹配。');result.replaceChildren(render(report,(side,eventId)=>evidence(side,eventId,t)));}
  async function run(){
   if(busy||!current||!comparison||!isActive())return;const t=++generation,r=current,c=comparison;
   busy=true;update();info.textContent='按固定规则与固定日志范围评分…';
   try{const report=await api('/kun-evaluations',{method:'POST',body:{rubricId:r.id,expectedRubricHash:r.hash,previewId:preview.id,against:c.against,leftThrough:c.left.through,rightThrough:c.right.through,expectedComparisonHash:c.hash}});if(!isActive()||t!==generation)return;if(report.rubric.hash!==r.hash||report.comparison.hash!==c.hash)throw Error('评分规则或范围身份不匹配。');show(report,t);if(!knownReports.has(report.id)){knownReports.add(report.id);history.append(button(report.rubric.title+' · '+report.createdAt+' · '+states[report.left.status]+' / '+states[report.right.status],()=>openReport(report.id)));}info.textContent='固定评分已保存；没有调用模型或工具。';}
   catch(e){if(isActive()&&t===generation)info.textContent=e.message;}
   finally{if(isActive()&&t===generation){busy=false;update();}}
  }
  async function load(kind){
   if(busy||!isActive()||(kind==='rubrics'?!rubricMore:!reportMore))return;const t=generation,offset=kind==='rubrics'?rubricOffset:reportOffset;busy=true;update();
   try{const page=await api('/kun-'+kind+'?'+new URLSearchParams({previewId:preview.id,offset,limit:20}));if(!isActive()||t!==generation)return;
    if(kind==='rubrics'){for(const r of page.items)if(!knownRules.has(r.id)){knownRules.add(r.id);choice.append(el('option',{value:r.id},r.title+' · '+r.ruleCount+' 项'));}rubricOffset=page.nextOffset;rubricMore=page.hasMore;}
    else{for(const v of page.items)if(!knownReports.has(v.id)){knownReports.add(v.id);history.append(button(v.title+' · '+v.createdAt+' · '+states[v.leftStatus]+' / '+states[v.rightStatus],()=>openReport(v.id)));}reportOffset=page.nextOffset;reportMore=page.hasMore;}
    info.textContent=page.items.length?'已读取保存项。'+(page.hasMore?'可继续读取下一页。':''):'没有更多保存项。';
   }catch(e){if(isActive()&&t===generation)info.textContent=e.message;}
   finally{if(isActive()&&t===generation){busy=false;update();}}
  }
  async function openReport(id){
   if(busy||!isActive())return;const t=++generation;busy=true;update();
   try{const report=await api('/kun-evaluations/'+enc(id));if(!isActive()||t!==generation)return;if(report.id!==id)throw Error('报告响应身份不匹配。');show(report,t);info.textContent='正在查看已保存报告，未重新读取来源日志。';}
   catch(e){if(isActive()&&t===generation)info.textContent=e.message;}
   finally{if(isActive()&&t===generation){busy=false;update();}}
  }
  function setComparison(value){generation++;busy=false;comparison=value;result.replaceChildren();if(current&&value&&current.baselineHash!==value.baseline.hash){current=null;choice.value='';fixedRule.replaceChildren();}info.textContent=value?'评分将固定使用上方对照的两侧范围。':'请先读取对照；已有报告仍可单独查看。';update();}
  addRow();info.textContent='先保存或选择固定规则，再读取上方对照并评分。';
  node.append(el('p',{class:'help'},'每份规则绑定同一来源安全点。按完整最终回复逐字匹配；缺失证据为未知，不把日志结束当作业务正确。当前没有独立验证集隔离。'),el('details',{},el('summary',{},'新建验收规则'),el('label',{},'规则标题',title),el('p',{},'固定项：运行成功结束（所有规则各占一项）'),rules,el('div',{class:'actions'},add,save)),el('div',{class:'actions'},loadRules,choice),fixedRule,evaluate,info,result,el('h4',{},'已保存评分报告'),loadReports,history);update();return {node,setComparison};
 }
 return {create,render};
})();
