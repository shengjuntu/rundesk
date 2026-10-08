// Explicit direct calls: no model turn, no implicit retries, replay never executes.
function mcpTestPanel(info, cp) {
 const tr=rdText;
 const server=el("select",{"aria-label":tr("测试服务器")},...Object.keys(info.effectiveServers||{}).map(n=>el("option",{value:n},n)));
 const tools=el("select",{"aria-label":tr("选择工具")});
 const args=el("textarea",{rows:7,"aria-label":tr("参数 JSON")},"{}");
 const schema=el("pre",{class:"mcp-test-output"},tr("读取工具后显示参数结构。"));
 const output=el("pre",{class:"mcp-test-output","aria-live":"polite"},tr("尚未运行测试。"));
 const confirm=el("input",{type:"checkbox"});
 const history=el("div",{class:"mcp-test-history"});
 let catalog=[],cursor="",busy=false;
 const load=button(tr("读取工具"),()=>run(false));
 const next=button(tr("下一页工具"),()=>run(false,true));next.hidden=true;
 const execute=button(tr("执行工具"),()=>run(true),"primary");execute.disabled=true;
 const showSchema=()=>{const tool=catalog.find(t=>t.name===tools.value);schema.textContent=json(tool?.inputSchema||{});execute.disabled=busy||!tool;};
 tools.onchange=showSchema;
 server.onchange=()=>{catalog=[];cursor="";tools.replaceChildren();schema.textContent="";execute.disabled=true;next.hidden=true;};
 async function refresh(){
  const rows=await api(cp("/mcp-tests"));
  history.replaceChildren(...rows.map(row=>el("div",{class:"skill-row"},el("span",{},`${new Date(row.created).toLocaleString()} · ${row.server} / ${row.tool||"tools/list"} · ${row.status} · ${row.durationMs} ms`),button(tr("回放记录"),async()=>{const record=await api(cp("/mcp-tests/"+row.id));output.textContent=tr("历史记录回放：未连接服务器、未重新执行。")+"\n\n"+json(record);}),button(tr("删除历史"),async()=>{await api(cp("/mcp-tests/"+row.id),{method:"DELETE"});await refresh();},"quiet"))));
  if(!rows.length)history.append(el("p",{class:"help"},tr("暂无测试记录。")));
 }
 async function run(call,more=false){
  if(busy)return;
  if(!confirm.checked)throw Error(tr("请先确认直接调用权限。"));
  const body={server:server.value,confirm:true};
  if(!body.server)throw Error(tr("请先配置 MCP 服务器。"));
  if(call){body.tool=tools.value;body.arguments=JSON.parse(args.value);if(!body.arguments||Array.isArray(body.arguments)||typeof body.arguments!=="object")throw Error(tr("参数必须是 JSON 对象。"));}
  else if(more)body.cursor=cursor;
  busy=true;load.disabled=execute.disabled=next.disabled=true;output.textContent=tr("正在连接并调用，最多等待 30 秒…");
  try{
   const record=await api(cp("/mcp-tests"),{method:"POST",body});output.textContent=json(record);
   if(!call&&record.status==="completed"){
    const incoming=record.result?.tools||[];catalog=more?[...catalog,...incoming]:incoming;
    tools.replaceChildren(...catalog.map(t=>el("option",{value:t.name},t.name)));cursor=record.result?.nextCursor||"";next.hidden=!cursor;showSchema();
   }
   await refresh();
  }catch(e){output.textContent=e.message;throw e;}finally{busy=false;load.disabled=next.disabled=false;showSchema();}
 }
 const panel=el("details",{class:"card mcp-test-panel"},el("summary",{},tr("MCP 独立测试台")),
  el("p",{class:"help"},tr("直接连接已配置的 MCP，不启动 Agent 对话。支持本机 stdio 和 Streamable HTTP；暂不支持容器、OAuth、sampling 或 elicitation。可测试已关闭的服务器，不会启用它。")),
  el("label",{},confirm,tr("我确认直接调用可能读写文件或访问外部服务，不经过 Agent 审批；参数和结果会保存，文本中的敏感信息需自行检查。")),
  el("div",{class:"row"},server,load,next),
  el("div",{class:"mcp-test-grid"},el("div",{},el("label",{},tr("选择工具"),tools),el("details",{},el("summary",{},tr("参数结构")),schema),el("label",{},tr("参数 JSON"),args),execute),el("div",{},el("h4",{},tr("调用结果")),output)),
  el("details",{},el("summary",{},tr("最近 50 条记录 · 回放不会重新执行")),el("p",{class:"help"},tr("历史删除不清除用于防止重复执行的 API 回执。")),button(tr("刷新记录"),refresh),history));
 panel.addEventListener("toggle",()=>{if(panel.open)safe(refresh);});return panel;
}
