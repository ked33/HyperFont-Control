export function toggleExemptPackage(profile, name, selected) {
  const values = new Set(profile.exemptPackages || []);
  if (selected) values.add(name); else values.delete(name);
  if (values.size > 128) throw new Error('应用豁免最多 128 个');
  if (values.size) profile.exemptPackages = [...values].sort();
  else delete profile.exemptPackages;
}

export function filterApps(apps, query) {
  const needle = query.trim().toLocaleLowerCase();
  return apps.filter(app => `${app.label} ${app.package}`.toLocaleLowerCase().includes(needle));
}

export function mountExemptions({request,run,getProfile,getStatus,markDirty}) {
  const el = (tag,text,cls) => { const node=document.createElement(tag);if(text!==undefined)node.textContent=text;if(cls)node.className=cls;return node; };
  const root=el('details',undefined,'grouped glass exemption-panel');
  root.append(el('summary','应用豁免 · 使用原生字体'),el('p','选择应用后，随当前方案提交并手动重启。需要 Android 12–15 和 Zygisk；独立 WebView、隔离进程需另行验收。','field-help'));
  const status=el('p',undefined,'field-help'),search=el('input');
  search.type='search';search.placeholder='搜索应用名称或包名';search.setAttribute('aria-label','搜索豁免应用');
  const reload=el('button','读取已安装应用','glass-button');reload.type='button';
  const list=el('div',undefined,'exemption-apps'),selected=el('div'),evidence=el('div');
  let apps=[];
  function refresh() {
    const packages=getProfile().exemptPackages||[],runtime=getStatus()?.appExemptions?.boot;
    status.textContent=`草稿 ${packages.length} 个 · 本次开机 ${runtime?.appIds?.length||0} 个 UID${runtime?.prepared?'，已准备共享原生映射':'，未准备豁免'}。`;
    selected.replaceChildren();list.replaceChildren();
    const known=new Set(apps.map(app=>app.package));
    for(const name of packages) {
      const row=el('div',undefined,'scheme-actions'),label=apps.find(app=>app.package===name)?.label;
      row.append(el('span',`${label||name}${apps.length&&!known.has(name)?' · 未安装':''}`));
      const remove=el('button','移除','glass-button');remove.type='button';remove.onclick=()=>{toggleExemptPackage(getProfile(),name,false);markDirty();refresh();};
      const inspect=el('button','检查豁免','glass-button');inspect.type='button';inspect.onclick=()=>run(async()=>{
        const result=await request({op:'exemption_evidence',package:name});evidence.replaceChildren(el('p','启动记录仅证明恢复步骤，界面文字仍需验收。'));
        if(!result.processes.length)evidence.append(el('p','没有当前运行进程的豁免记录，请在重启生效后打开应用。'));
        const labels={'files-restored':'原生字体文件已恢复','font-map-substituted':'原生字体映射已换入','initialization-hook-removed':'初始化适配已撤销','awaiting-font-map':'等待系统字体映射','map-unavailable':'原生映射不可用','unsupported-initialization':'初始化方式不支持','initialization-hook-unavailable':'初始化适配不可用','files-restore-failed':'文件恢复失败','font-map-substitution-failed':'映射换入失败'};
        for(const process of result.processes)evidence.append(el('p',`PID ${process.pid} · ${process.milestones.map(value=>labels[value]||value).join(' → ')}`));
      });row.append(remove,inspect);selected.append(row);
    }
    for(const warning of runtime?.warnings||[])selected.append(el('p',warning,'field-help'));
    for(const app of filterApps(apps,search.value)) {
      const row=el('label',undefined,'exemption-app'),check=el('input');check.type='checkbox';check.checked=packages.includes(app.package);
      check.onchange=()=>{try{toggleExemptPackage(getProfile(),app.package,check.checked);markDirty();refresh();}catch(error){check.checked=!check.checked;evidence.textContent=error.message;}};
      const text=el('span');text.append(el('strong',app.label),el('small',`${app.package} · UID ${app.uid}`));row.append(check,text);list.append(row);
    }
  }
  reload.onclick=()=>run(async()=>{apps=await request({op:'installed_apps'});refresh();});
  search.oninput=refresh;
  root.append(status,selected,reload,search,list,evidence);
  document.querySelector('#settings').append(root);
  refresh();return {refresh};
}
