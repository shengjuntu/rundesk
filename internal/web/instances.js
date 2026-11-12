"use strict";

// Settings and legacy links resolve the same environment/workspace pair. Opening
// a link reads registered metadata only, never starting an engine or container.
window.RunDeskInstances = (() => {
  const rememberedKey = iid => "rundesk-instance-workspace:" + iid;

  function remember(iid, wid) {
    if (!iid || !wid) return;
    try { localStorage.setItem(rememberedKey(iid), wid); } catch {}
  }

  function remembered(iid) {
    try { return localStorage.getItem(rememberedKey(iid)); } catch { return ""; }
  }

  function binding(iid, wid) {
    for (const a of state.applications) {
      const p = (a.runtimePresets || []).find(p => p.instanceId === iid);
      if (p) return {...a, instanceId:p.instanceId, workspaceId:p.workspaceId, presetId:p.id, presetName:p.name, enabled:p.enabled};
    }
    const apps = state.applications.filter(a => a.instanceId === iid);
    const a = apps.find(a => a.workspaceId === wid) || (apps.length === 1 ? apps[0] : null);
    return a ? {...a, presetId:""} : null;
  }

  function source(iid, wid = ws()?.id) {
    const b = binding(iid, wid);
    if (b?.presetId && !b.enabled) throw Error(rdText("关联的应用预设已停用，历史仍可查看。"));
    if (b?.presetId && wid !== b.workspaceId) throw Error(rdText("这个工作区不能用于当前实例，请重新选择。"));
    return b?.presetId ? {kind:"application", appId:b.appId, presetId:b.presetId} : undefined;
  }

  function workspacesFor(i) {
    const b = binding(i.id);
    return state.workspaces.filter(w => {
      if (w.purpose === "maintenance") return false;
      const owner = RunDeskConversations.owner(w.id);
      if (owner && owner.instanceId !== i.id) return false;
      return !b?.presetId || (owner?.appId === b.appId && owner.presetId === b.presetId);
    });
  }

  function chooseWorkspace(i, wid) {
    const choices = workspacesFor(i);
    if (wid) {
      const selected = choices.find(w => w.id === wid);
      if (!selected) throw Error(rdText("这个工作区不能用于当前实例，请重新选择。"));
      return selected;
    }
    const b = binding(i.id);
    const ids = [b?.presetId ? b.workspaceId : "", remembered(i.id), ...(state.environments || []).filter(e => e.instanceId === i.id).map(e => e.workspaceId), b?.workspaceId];
    for (const id of ids) {
      const work = choices.find(w => w.id === id);
      if (work) return work;
    }
    return choices[0] || null;
  }

  function select(iid, wid) {
    const i = state.instances.find(row => row.id === iid);
    if (!i) throw Error(rdText("实例不存在，请刷新列表。"));
    const w = chooseWorkspace(i, wid);
    renderInstanceOptions(i.id);
    state.skills = []; state.mcp = null; $("#models")?.replaceChildren();
    $("#workspace").replaceChildren(...workspacesFor(i).map(work => el("option", {value:work.id}, work.name)));
    $("#workspace").value = w?.id || "";
    remember(i.id, w?.id);
    updateWorkspaceLabel();
    return {instance:i, workspace:w, binding:binding(i.id, w?.id)};
  }

  async function loadCatalog() {
    if ($("#settings").open) $("#settings").close();
    resetConversation();
    const request = state.selection;
    $("#instance").value = "";
    $("#workspace").value = "";
    const [instances, workspaces, applications, environments] = await Promise.all(
      ["/instances", "/workspaces", "/applications", "/environments"].map(path => api(path))
    );
    const projects = await Promise.all(applications.map(a => api("/applications/" + encodeURIComponent(a.appId) + "/project-environments")));
    if (request !== state.selection) return false;
    Object.assign(state, {instances, workspaces, applications, environments, projectEnvironments:projects.flat()});
    return true;
  }

  async function resolveLegacy(iid, wid, action = "", tab = "") {
    if (!await loadCatalog()) return;
    if (state.instances.find(row => row.id === iid)?.agentRuntime?.kind === "kun") {
      return RunDeskOnboarding.open({step:"llm"});
    }
    const {instance:i, workspace:w, binding:b} = select(iid, wid);
    if (action === "conversation" && !(b?.presetId && !b.enabled)) {
      if (!w) throw Error(rdText("先创建一个工作区，再开始会话。"));
      return RunDeskConversations.open(w.id, {instanceId:i.id});
    }
    if (b && b.workspaceId === w?.id && (i.id !== "default" || wid)) {
      await showApplication(b.appId, b.presetId || "", "settings");
      if (action === "settings" && tab) await openSettings(tab);
      else if (action === "environments") await RunDeskEnvironments.open({...b, returnToSettings:true});
      return;
    }
    if (i.id === "default") {
      await RunDeskConversations.open(w?.id, {fresh:true, instanceId:i.id});
      if (ws()) await openSettings(tab || "instances");
      return;
    }
    return RunDeskBuildCenter.openEnvironment(i.id, w?.id, action === "environments" ? "containers" : tab || "instances");
  }

  async function legacy(...args) {
    try {
      return await resolveLegacy(...args);
    } catch (error) {
      if (state.view === "conversation" && /^#(?:instance|legacy)\//.test(location.hash)) {
        state.draftRoutingError = error.message;
        renderStatus();
      }
      throw error;
    }
  }

  return {binding, source, workspacesFor, select, remember, loadCatalog, legacy};
})();
