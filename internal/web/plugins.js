"use strict";
window.RunDeskNativePlugins = (() => {
  const enc = encodeURIComponent;
  const origin = value => value?.pluginId ? rdText("原生插件：") + (value.pluginDisplayName || value.pluginId) : "";
  function originNode(value) { const text = origin(value); return text ? el("span", {class:"native-plugin-origin"}, text + rdText(" · 由插件管理")) : null; }
  async function render(target) {
    const iid = instance()?.id, wid = ws()?.id;
    const data = await api("/instances/" + enc(iid) + "/plugins" + (wid ? "?workspaceId=" + enc(wid) : ""));
    if (state.settingsTab !== "plugins" || instance()?.id !== iid || ws()?.id !== wid || !target.isConnected) return;
    const inventory = el("section", {class:"card"}, el("h3", {}, rdText("原生插件")), el("p", {class:"native-plugin-note"}, rdText("插件可以提供 Skills、MCP 和其他资源。这里读取配置与已观察到的组件；配置启用、组件被发现与插件完整安装是不同状态。")));
    for (const warning of data.warnings || []) inventory.append(el("p", {class:"help"}, warning.message));
    if (data.backend !== "codex") inventory.append(el("p", {class:"help"}, rdText("此引擎不支持 Codex 原生插件。仍可检查插件包内容。")));
    for (const plugin of data.plugins || []) {
      const row = el("article", {class:"native-plugin-row","data-native-plugin-id":plugin.id}, el("h4", {}, plugin.displayName || plugin.id), el("p", {class:"help"}, plugin.id), el("span", {class:"badge"}, plugin.configured ? rdText("有配置") : rdText("未记录配置")), el("span", {class:"badge"}, plugin.enabled === true ? rdText("配置已启用") : plugin.enabled === false ? rdText("配置已禁用") : rdText("启用状态未知")), el("span", {class:"badge"}, plugin.loaded === "observed" ? rdText("已观察到组件") : rdText("加载状态未知")), el("p", {class:"native-plugin-note"}, rdText("完整安装清单：未知。此页面不执行安装、卸载或启停。")));
      const components = el("div", {class:"native-plugin-components"});
      for (const skill of plugin.skills || []) components.append(el("p", {}, "Skill · " + skill.name, skill.path ? el("small", {class:"native-plugin-origin"}, skill.path) : null));
      for (const server of plugin.mcpServers || []) components.append(el("p", {}, "MCP · " + server.name + (server.runtimeStatus ? " · " + server.runtimeStatus : "")));
      row.append(components); inventory.append(row);
    }
    if (!data.plugins?.length) inventory.append(el("p", {class:"empty"}, rdText("没有发现插件配置或归属于插件的组件；这不代表完整原生插件清单为空。")));
    target.replaceChildren(inventory, inspection());
  }
  function inspection() {
    const section = el("section", {class:"card plugin-inspection"}), file = el("input", {id:"native-plugin-zip",type:"file",accept:".zip,application/zip",required:true}), error = el("p", {class:"error",role:"status"}), result = el("div", {class:"plugin-inspection-result",role:"status"}), submit = el("button", {type:"submit",class:"primary"}, rdText("检查插件包")), form = el("form", {}, el("label", {}, rdText("原生插件 ZIP"), file), el("p", {class:"help"}, rdText("读取原生 manifest、组件和依赖，只做静态检查。不会执行包内脚本或安装插件。")), error, submit);
    let busy = false, sequence = 0;
    file.onchange = () => { ++sequence; result.replaceChildren(); error.textContent = ""; };
    form.onsubmit = async event => {
      event.preventDefault(); if (busy) return; const zip = file.files[0]; if (!zip) return;
      const seq = ++sequence; busy = true; submit.disabled = true; error.textContent = ""; result.replaceChildren();
      try {
        if (zip.size > 16 * 1048576) throw Error(rdText("ZIP 最大 16 MiB"));
        const body = new FormData(); body.append("file", zip);
        const value = await api("/plugins/inspect", {method:"POST",body}); if (seq !== sequence || !section.isConnected) return;
        result.append(el("h4", {}, value.manifest.name + (value.manifest.version ? " · " + value.manifest.version : "")), el("p", {class:"help"}, value.manifest.description || ""), el("p", {class:"help"}, value.format + " · " + value.manifestPath), el("p", {class:"native-plugin-note"}, rdText("静态检查完成。安装状态：未执行；运行兼容性需在目标实例验证。")));
        for (const component of value.components || []) result.append(el("p", {class:"native-plugin-components"}, component.kind + " · " + component.name + (component.path ? " · " + component.path : "")));
        if (value.dependencies?.length) result.append(el("h4", {}, rdText("依赖声明")), ...value.dependencies.map(dependency => el("p", {class:"help"}, dependency.name + (dependency.requirement ? " · " + dependency.requirement : ""))));
        for (const warning of value.warnings || []) result.append(el("p", {class:"help"}, typeof warning === "string" ? warning : warning.message));
        if (value.unknownFields?.length) result.append(el("p", {class:"help"}, rdText("未识别字段：") + value.unknownFields.join(", ")));
      } catch (e) { if (seq === sequence && section.isConnected) error.textContent = e.message; }
      finally { busy = false; submit.disabled = false; }
    };
    section.append(el("h3", {}, rdText("检查原生插件包")), form, result); return section;
  }
  function mcpOrigins(info) {
    const rows = new Map();
    for (const [name, value] of Object.entries(info.componentOrigins || {})) if (value?.pluginId) rows.set(name, value);
    for (const value of info.status?.data || []) if (value.pluginId) rows.set(value.name, {type:"native_plugin",pluginId:value.pluginId});
    if (!rows.size) return null;
    return el("section", {class:"card"}, el("h3", {}, rdText("插件提供的 MCP")), el("p", {class:"native-plugin-note"}, rdText("这些服务归属于原生插件，在此只读展示。运行工具状态不代表插件已完成安装验证。")), ...[...rows].map(([name, value]) => el("div", {class:"skill-row"}, el("div", {}, el("strong", {}, name), originNode(value)))));
  }
  return {render, origin, originNode, mcpOrigins};
})();
