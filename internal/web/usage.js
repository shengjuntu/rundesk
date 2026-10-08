window.RunDeskUsage=(()=>{
 const tr=rdText;
 function dateValue(d){return `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;}
 function open(){
  const d=el('dialog',{class:'usage-dialog'}),now=new Date(),start=new Date(now);start.setDate(start.getDate()-29);
  const from=el('input',{type:'date',value:dateValue(start),'aria-label':tr('开始日期')}),to=el('input',{type:'date',value:dateValue(now),'aria-label':tr('结束日期')});
  const app=el('select',{'aria-label':tr('应用筛选')},el('option',{value:''},tr('全部应用与助手')),...state.applications.map(a=>el('option',{value:a.appId},a.name)));
  const work=el('select',{'aria-label':tr('项目筛选')},el('option',{value:''},tr('全部项目')),...state.workspaces.map(w=>el('option',{value:w.id},w.name)));
  const content=el('div',{class:'usage-content'}),error=el('p',{class:'error',role:'status'}),stamp=el('p',{class:'help'});let report=null,busy=false;
  const exportButton=button(tr('导出统计 JSON'),()=>{if(!report)return;const url=URL.createObjectURL(new Blob([JSON.stringify(report,null,2)],{type:'application/json'}));const a=el('a',{href:url,download:'rundesk-usage.json'});a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);});exportButton.disabled=true;
  const load=button(tr('查询用量'),refresh,'primary');
  async function refresh(){
   if(busy)return;busy=true;load.disabled=exportButton.disabled=true;error.textContent='';
   try{
    if(!from.value||!to.value)throw Error(tr('请选择完整日期范围。'));
    const lower=new Date(from.value+'T00:00:00'),upper=new Date(to.value+'T00:00:00');upper.setDate(upper.getDate()+1);
    const q=new URLSearchParams({from:lower.toISOString(),to:upper.toISOString(),appId:app.value,workspaceId:work.value});
    report=await api('/usage?'+q);render(report);stamp.textContent=tr('统计快照：')+new Date(report.generatedAt).toLocaleString()+' · '+tr('截至事件：')+report.throughEventId;exportButton.disabled=false;
   }catch(e){error.textContent=e.message;report=null;content.replaceChildren();stamp.textContent='';}finally{busy=false;load.disabled=false;}
  }
  function render(r){
   const total=r.rows.reduce((n,row)=>n+(row.tokens.totalTokens??0),0),known=r.rows.some(row=>row.tokens.totalTokens!==null),runs=r.rows.reduce((n,row)=>n+row.runs,0),missing=r.rows.reduce((n,row)=>n+row.unknownRuns,0);
   const metric=(name,value)=>el('div',{class:'usage-metric'},el('small',{},name),el('strong',{},value));
   const fmt=n=>n===null||n===undefined?tr('未知'):n.toLocaleString();
   const issueLabels={unknown_baseline:tr('起始累计值未知'),missing_snapshot:tr('缺少累计快照'),missing_total:tr('缺少总 token'),invalid_counter:tr('无效计数'),counter_regression:tr('累计值回退'),ambiguous_owner:tr('线程归属冲突'),missing_field_baseline:tr('字段起始值未知'),unmatched_turn:tr('无法匹配执行轮次')};
   const table=el('table',{class:'usage-table'},el('thead',{},el('tr',{},...[tr('应用 / 项目'),tr('已观测 token'),tr('输入 token'),tr('输出 token'),tr('有上报 / 执行轮次')].map(x=>el('th',{},x)))),el('tbody',{},...r.rows.map(row=>{
    const name=state.applications.find(a=>a.appId===row.appId)?.name||(row.appId||tr('通用助手')),workspace=state.workspaces.find(w=>w.id===row.workspaceId)?.name||row.workspaceId;
    const detail=el('details',{},el('summary',{},name+' · '+workspace),el('p',{class:'help'},tr('缓存输入（输入子集）：')+fmt(row.tokens.cachedInputTokens)+' · '+tr('推理输出（输出子集）：')+fmt(row.tokens.reasoningOutputTokens)),el('p',{class:'help'},Object.entries(row.statuses).map(([k,v])=>(statusLabel[k]||(k==='unfinished'?tr('尚无结束记录'):k))+': '+v).join(' · ')),el('p',{class:'help'},Object.entries(row.issues).map(([k,v])=>(issueLabels[k]||k)+': '+v).join(' · ')||tr('未发现计数异常；有上报不代表账单完整。')));
    return el('tr',{},el('td',{},detail),el('td',{},fmt(row.tokens.totalTokens)),el('td',{},fmt(row.tokens.inputTokens)),el('td',{},fmt(row.tokens.outputTokens)),el('td',{},`${row.reportedRuns} / ${row.runs}`));
   })));
   content.replaceChildren(...(r.demo?[el('p',{class:'demo-banner'},tr('演示模式：数字来自测试协议，不是真实模型消耗。'))]:[]),el('div',{class:'usage-metrics'},metric(tr('已观测 token'),known?total.toLocaleString():tr('未知')),metric(tr('执行轮次'),runs.toLocaleString()),metric(tr('未取得用量的轮次'),missing.toLocaleString())),r.rows.length?el('div',{class:'usage-table-scroll'},table):el('p',{class:'empty'},tr('此范围没有保留的执行或用量记录。')));
  }
  d.append(el('div',{class:'dialog-head'},el('h2',{},tr('用量统计')),button(tr('关闭'),()=>d.close(),'quiet')),el('p',{class:'help'},tr('按累计值的已观测增量统计，不是费用或硬性配额。缺失字段显示未知；缓存输入与推理输出不重复加到总数。')),el('div',{class:'usage-filters'},el('label',{},tr('开始日期'),from),el('label',{},tr('结束日期'),to),app,work,load),el('p',{class:'help'},tr('日期采用浏览器本地时区，包含结束日期。token 按收到事件的时间归属；轮次按启动时间筛选。删除会话会移除对应统计。')),error,content,el('div',{class:'usage-footer'},stamp,exportButton));
  d.onclose=()=>d.remove();document.body.append(d);d.showModal();refresh();return d;
 }
 return {open};
})();
