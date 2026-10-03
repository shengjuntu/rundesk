/* Readable projections of recorded events. No inferred plans or success claims. */
(function (root, factory) {
  if (typeof module === "object" && module.exports) module.exports = factory();
  else root.RunDeskProcess = factory();
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";
  const text = (v) => v == null ? "" : typeof v === "string" ? v : JSON.stringify(v, null, 2);
  const present = (...values) => values.find(v => v !== undefined && v !== null);
  function inspect(row) {
    const d = row.detail || {};
    let input, output;
    switch (row.type) {
      case "commandExecution": input = {command:d.command, cwd:d.cwd}; output = d.aggregatedOutput; break;
      case "mcpToolCall":
      case "dynamicToolCall": input = d.arguments; output = present(d.result, d.contentItems, d.content); break;
      case "webSearch": input = present(d.action, d.query); output = present(d.results, d.result, d.content); break;
      case "fileChange": input = d.changes; output = d.aggregatedOutput; break;
      case "agentMessage": output = d.text; break;
      case "reasoning": output = (d.summary?.length ? d.summary : d.content || []).map(p => typeof p === "string" ? p : p.text || "").join("\n\n"); break;
      default: output = row.body;
    }
    const errorContent=d.result?.isError && Array.isArray(d.result.content) ? d.result.content.filter(p=>p.type==="text").map(p=>p.text).join("\n") : "";
    const error = text(d.error || errorContent || (d.result?.isError ? d.result : null));
    if(errorContent)output=errorContent;
    return {input:text(input), output:text(output), error,
      empty: Array.isArray(output) && output.length === 0 ||
        !!output && typeof output === "object" && ["results", "items", "matches"].some(k => Array.isArray(output[k]) && output[k].length === 0)};
  }
  function replyRows(rows) {
    const replies = rows.filter(r => r.type === "agentMessage" && r.detail?.phase !== "commentary");
    const final = replies.filter(r => r.detail?.phase === "final_answer");
    return final.length ? final : replies.slice(-1);
  }
  function links(rows) {
    const found = new Map();
    for (const row of rows.filter(r => r.track === "tools")) {
      const output = inspect(row).output;
      for (const match of output.matchAll(/https?:\/\/[^\s<>"'`\\]+/g)) {
        let value = match[0].replace(/[),.;\]}，。；）]+$/, "");
        try {
          const u = new URL(value);
          if (!["https:", "http:"].includes(u.protocol) || u.username || u.password) continue;
          value = u.href;
        } catch { continue; }
        if (!found.has(value)) found.set(value, {url:value, rows:[]});
        const record = found.get(value);
        if (!record.rows.includes(row.id)) record.rows.push(row.id);
        if (found.size >= 100) return [...found.values()];
      }
    }
    return [...found.values()];
  }
  return {inspect, replyRows, links, text};
});
