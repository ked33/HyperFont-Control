export function mountSchemes({request,run,toast,confirmAction,currentProfile,getDirty,loadStatus,available,loadProfile,staged}) {
  const el=(tag,text,cls)=>{const e=document.createElement(tag);if(text!==undefined)e.textContent=text;if(cls)e.className=cls;return e;};
  const root=el('section',undefined,'scheme-panel glass'),list=el('div',undefined,'scheme-list');
  const button=(label,fn,primary=false)=>{const b=el('button',label,primary?'primary':'glass-button');b.type='button';b.disabled=!available;b.onclick=()=>run(fn);return b;};
  root.append(el('h2','我的方案'),el('p','保存完整的四类字体和字重，一键切换后手动重启生效。','field-help'));
  const actions=el('div',undefined,'scheme-actions');
  actions.append(button('保存为新方案',async()=>{await request({op:'save_preset',profile:currentProfile()});await refresh();toast('已保存新方案');}));root.append(actions,list);
  const backup=el('details'),backupSummary=el('summary','字体与方案备份还原'),backupActions=el('div',undefined,'scheme-actions');
  backup.append(backupSummary,el('p','ZIP 包含全部已导入字体、方案与已保存配置快照。还原采用合并方式，随后可在方案列表中选择切换。','field-help'));
  const progress=el('progress');progress.max=100;progress.hidden=true;const status=el('p','','field-help');status.setAttribute('role','status');
  backupActions.append(button('完整备份 ZIP',async()=>{status.textContent='正在备份已保存数据…';const r=await request({op:'export_backup'});status.textContent=`备份已保存：${r.path}`;toast('完整备份已保存到 Download');}));
  const downloads=el('div',undefined,'scheme-list');
  backupActions.append(button('从 Download 还原',async()=>{const files=await request({op:'list_backups'});downloads.replaceChildren();if(!files.length)downloads.append(el('p','Download 中没有 ZIP 文件','field-help'));for(const f of files)downloads.append(button(`${f.name} · ${(f.size/1048576).toFixed(1)} MiB`,()=>restore({path:f.path})));}));
  const pickerLabel=el('label','其他位置…','file-button glass-button'),picker=el('input');picker.type='file';picker.accept='.zip,application/zip';picker.disabled=!available;pickerLabel.append(picker);backupActions.append(pickerLabel);
  backup.append(backupActions,progress,status,downloads);root.append(backup);document.querySelector('#settings-overview').append(root);
  async function choose(title,message,choices){
    const dialog=el('dialog',undefined,'glass');dialog.append(el('h2',title),el('p',message));const row=el('div',undefined,'scheme-actions');dialog.append(row);document.body.append(dialog);
    return new Promise(resolve=>{const finish=v=>{dialog.close();dialog.remove();resolve(v);};for(const [value,label] of choices){const b=el('button',label,'glass-button');b.onclick=()=>finish(value);row.append(b);}dialog.oncancel=e=>{e.preventDefault();finish(null);};dialog.showModal();});
  }
  async function beforeReplace(){
    if(!getDirty())return true;
    const answer=await choose('有尚未保存的编辑','是否先把当前编辑保存成新方案？',[['save','保存后继续'],['discard','放弃编辑并继续'],[null,'取消']]);
    if(!answer)return false;
    if(answer==='save'){await request({op:'save_preset',profile:currentProfile()});await refresh();}
    return true;
  }
  async function rename(p){
    const dialog=el('dialog',undefined,'glass'),input=el('input');input.value=p.profile.name;input.maxLength=80;input.setAttribute('aria-label','方案名称');dialog.append(el('h2','重命名方案'),input);
    const row=el('div',undefined,'scheme-actions');dialog.append(row);document.body.append(dialog);
    const value=await new Promise(resolve=>{const finish=v=>{dialog.close();dialog.remove();resolve(v);};const save=el('button','保存','primary'),cancel=el('button','取消','glass-button');save.onclick=()=>{if(input.value.trim())finish(input.value.trim());};cancel.onclick=()=>finish(null);dialog.oncancel=e=>{e.preventDefault();finish(null);};row.append(cancel,save);dialog.showModal();input.focus();});
    if(value){await request({op:'save_preset',id:p.id,profile:{...p.profile,name:value}});await refresh();}
  }
  async function refresh(){
    const items=await request({op:'list_presets'});list.replaceChildren();
    if(!items.length)list.append(el('p','还没有方案。调整字体后，点击“保存为新方案”。','field-help'));
    for(const p of items){
      const item=el('article',undefined,'scheme-item'),row=el('div',undefined,'scheme-actions');item.append(el('strong',p.profile.name));
      const modes={system:'系统',variable:'可变',static:'静态'},names={chinese:'中文',latinSans:'无衬线',serif:'衬线',monospace:'等宽'};
      item.append(el('p',Object.entries(p.profile.slots).map(([k,v])=>`${names[k]}：${modes[v.mode]}`).join(' · ')));
      row.append(button('一键切换',async()=>{if(!await beforeReplace())return;await request({op:'stage_preset',id:p.id});await staged();toast('方案已提交，请手动重启生效');},true));
      row.append(button('载入编辑',async()=>{if(await beforeReplace()){loadProfile(p.profile);toast('方案已载入编辑，尚未提交');}}));
      row.append(button('用当前设置更新',async()=>{if(await confirmAction('更新方案',`用当前四类字体设置覆盖“${p.profile.name}”？`)){await request({op:'save_preset',id:p.id,profile:{...currentProfile(),name:p.profile.name}});await refresh();toast('方案已更新');}}));
      row.append(button('重命名',()=>rename(p)),button('删除',async()=>{if(await confirmAction('删除方案',`删除“${p.profile.name}”？字体文件及当前生效配置会保留。`)){await request({op:'delete_preset',id:p.id});await refresh();}}));
      item.append(row);list.append(item);
    }
  }
  async function restore(source){
    let prepared;
    try{
      status.textContent='正在校验备份中的字体与方案…';
      prepared=await request({op:'prepare_restore',...source});
      const ok=await confirmAction('合并还原',`备份包含 ${prepared.fonts} 个字体、${prepared.presets} 套方案、${prepared.snapshots} 份配置快照。相同字体去重，冲突方案保留副本；当前编辑和生效状态保持不变。`);
      if(!ok){status.textContent='已取消还原';return;}
      const r=await request({op:'merge_restore',token:prepared.token});prepared=null;
      await loadStatus(true);await refresh();status.textContent=`还原完成：新增 ${r.fonts} 个字体、${r.presets} 套方案。选择方案后可一键切换。`;toast('备份已合并还原');
    }finally{if(prepared)try{await request({op:'cancel_restore',token:prepared.token});}catch(e){status.textContent=`清理临时还原失败：${e.message}`;}}
  }
  picker.onchange=()=>run(async()=>{
    const file=picker.files[0];if(!file)return;let upload;
    try{
      if(file.size>4*1024**3)throw new Error('备份不能超过 4 GiB');
      upload=await request({op:'upload_begin',kind:'backup',label:file.name,size:file.size});progress.hidden=false;
      for(let offset=0;offset<file.size;offset+=48*1024){const bytes=new Uint8Array(await file.slice(offset,offset+48*1024).arrayBuffer());let binary='';for(const b of bytes)binary+=String.fromCharCode(b);await request({op:'upload_chunk',token:upload.token,offset,chunk:btoa(binary)});progress.value=(offset+bytes.length)/file.size*100;status.textContent=`上传备份 ${Math.round(progress.value)}%`;}
      await restore({token:upload.token});
    }finally{picker.value='';progress.hidden=true;if(upload)try{await request({op:'upload_cancel',token:upload.token});}catch{/* The next cleanup can reclaim the uploaded archive. */}}
  });
  return {refresh};
}
