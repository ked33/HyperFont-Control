export const roleLabels = {sans:'无衬线',serif:'衬线',monospace:'等宽',casual:'随意手写',cursive:'连笔手写',decorative:'装饰',emoji:'彩色表情',symbols:'数学与技术符号',icons:'图标'};
export const scriptLabels = {Hani:'汉字',Latn:'拉丁文字',Grek:'希腊文',Cyrl:'西里尔文',Hira:'平假名',Kana:'片假名',Hang:'韩文',Arab:'阿拉伯文',Hebr:'希伯来文',Deva:'天城文',Thai:'泰文',Bopo:'注音',Hans:'简体中文',Hant:'繁体中文'};
export function faceEligible(face, rule, fallback = false) {
  if (!rule) return true;
  if (rule.role === 'emoji') return !!face.emoji && !!face.colorFormats?.length;
  if (rule.role === 'symbols') return !!face.symbols;
  if (rule.role === 'icons') return !!face.privateUse;
  return rule.scripts.some(script => face.scripts?.includes(script) || script === 'Hani' && face.chinese || script === 'Latn' && face.latin);
}
export function makeFontSlot(assetId, face) {
  const axis = (face.axes||[]).find(a => a.tag === 'wght');
  if (axis) return {mode:'variable',assetId,faceIndex:face.index,familyMode:'instances',weightMap:Object.fromEntries(Array.from({length:9},(_,i)=>(i+1)*100).map(w=>[w,Math.max(axis.min,Math.min(axis.max,w))]))};
  return {mode:'static',missingWeightPolicy:'nearest',faces:[{assetId,faceIndex:face.index,weight:Math.max(100,Math.min(900,Math.round(face.weight/100)*100)),style:face.italic?'italic':'normal'}]};
}
export function editorEntries(profile, core) {
  const rules = profile.coverage?.rules || [];
  return [...core,...rules.map(rule=>[`rule:${rule.id}`,rule.label || `${roleLabels[rule.role]} · ${(rule.scripts||[]).map(s=>scriptLabels[s]||s).join('、')}`,'字','高级覆盖规则',''])];
}
export function findRule(profile,key) { return key?.startsWith('rule:') ? profile.coverage?.rules?.find(r=>r.id===key.slice(5)) : null; }
export function getSlot(profile,key) { return findRule(profile,key)?.font || profile.slots[key]; }
export function putSlot(profile,key,slot) { const rule=findRule(profile,key);if(rule)rule.font=slot;else profile.slots[key]=slot; }

export function mountCoverage({request,run,toast,getProfile,getFonts,getCapabilities,markDirty,openRule,renderSlots}) {
  const el=(tag,value,cls)=>{const e=document.createElement(tag);if(value!==undefined)e.textContent=value;if(cls)e.className=cls;return e;};
  const root=el('details',undefined,'coverage-settings glass');root.append(el('summary','扩大字体覆盖'));
  const body=el('div',undefined,'coverage-body');root.append(body);document.querySelector('#settings-overview').append(root);
  function toggle(label,value,change){const row=el('label',undefined,'coverage-toggle'),input=el('input');input.type='checkbox';input.checked=value;input.onchange=()=>{change(input.checked);markDirty();renderSlots();};row.append(input,el('span',label));return row;}
  const config=()=>{const c=getProfile().coverage ||= {autoFamilies:false,engineCompatibility:false};c.rules ||= [];return c;};
  function addRule(existing){
    const dialog=el('dialog',undefined,'glass coverage-dialog'),title=el('h2',existing?'编辑覆盖规则':'新增覆盖规则');dialog.append(title);
    const role=el('select');role.setAttribute('aria-label','字体用途');for(const [value,label]of Object.entries(roleLabels)){const o=el('option',label);o.value=value;role.append(o);}
    const scripts=el('select');scripts.multiple=true;scripts.size=7;scripts.setAttribute('aria-label','文字系统');
    const known=getCapabilities()?.scripts || ['Hani','Latn','Grek','Cyrl','Hira','Kana','Hang','Arab','Hebr','Thai'];
    for(const value of known){const o=el('option',scriptLabels[value]||value);o.value=value;o.selected=existing?(existing.scripts||[]).includes(value):value==='Hani';scripts.append(o);}
    const label=el('input');label.placeholder='规则名称';label.maxLength=80;label.setAttribute('aria-label','规则名称');
    const languages=el('input');languages.placeholder='语言，如 zh-Hans、ja；用逗号分隔';languages.value='zh-Hans';languages.setAttribute('aria-label','语言标签');
    const families=el('input');families.placeholder='指定系统字体族，可留空；用逗号分隔';families.setAttribute('aria-label','目标字体族');
    if(existing){role.value=existing.role;label.value=existing.label||'';languages.value=(existing.languages||[]).join(',');families.value=(existing.families||[]).join(',');}
    dialog.append(role,scripts,languages,families,label,el('p','汉字需要指定语言。图标必须指定字体族，并确认编码含义一致。','field-help'));
    role.onchange=()=>{const special=['emoji','symbols','icons'].includes(role.value);scripts.disabled=special;languages.disabled=special;};
    const actions=el('div',undefined,'scheme-actions'),save=el('button',existing?'保存':'新增','primary'),cancel=el('button','取消','glass-button');actions.append(cancel,save);dialog.append(actions);document.body.append(dialog);
    const close=()=>{dialog.close();dialog.remove();};cancel.onclick=close;dialog.oncancel=()=>dialog.remove();
    save.onclick=()=>{const special=['emoji','symbols','icons'].includes(role.value),selected=special?[]:[...scripts.selectedOptions].map(o=>o.value);const split=v=>v.split(/[,，]+/).map(x=>x.trim()).filter(Boolean);if(!special&&!selected.length){toast('请选择文字系统');return;}const langs=special?[]:split(languages.value),targets=split(families.value);if(selected.includes('Hani')&&!langs.length){toast('汉字覆盖需要语言标签');return;}if(role.value==='icons'&&!targets.length){toast('图标需要指定具体字体族');return;}if(!existing&&config().rules.length>=32){toast('最多 32 条规则');return;}const id=existing?.id||`rule-${Date.now().toString(36)}`;const rule={id,label:label.value.trim(),role:role.value,scripts:selected,languages:langs,families:targets,font:existing?.font||{mode:'system'}};if(existing)Object.assign(existing,rule);else config().rules.push(rule);markDirty();close();refresh();renderSlots();openRule(`rule:${id}`);};role.onchange();dialog.showModal();
  }
  function refresh(){body.replaceChildren();const p=getProfile(),c=p.coverage||{};
    body.append(toggle('扩展到其他系统文字字体族',!!c.autoFamilies,v=>config().autoFamilies=v),toggle('兼容内置 Flutter / Skia 的中文回退',!!c.engineCompatibility,v=>config().engineCompatibility=v),el('p','保留原生补字。应用私有字库和图片文字需另外适配；兼容开关的效果需重启后验证。','field-help'));
    const add=el('button','新增语言、用途或特殊字体规则','glass-button');add.type='button';add.onclick=()=>addRule();body.append(add);
    for(const rule of c.rules||[]){const row=el('div',undefined,'coverage-rule');row.append(el('strong',rule.label||`${roleLabels[rule.role]} ${(rule.scripts||[]).join('、')}`));const edit=el('button','字体与字重','glass-button'),remove=el('button','移除','glass-button');edit.onclick=()=>openRule(`rule:${rule.id}`);remove.onclick=()=>{config().rules=config().rules.filter(r=>r.id!==rule.id);markDirty();refresh();renderSlots();};const settings=el('button','规则设置','glass-button');settings.onclick=()=>addRule(rule);const up=el('button','上移','glass-button');up.onclick=()=>{const list=config().rules,i=list.indexOf(rule);if(i>0){[list[i-1],list[i]]=[list[i],list[i-1]];markDirty();refresh();renderSlots();}};row.append(settings,edit,up,remove);body.append(row);}
  }
  const reportRoot=document.querySelector('#coverage-page');reportRoot.replaceChildren();
  const controls=el('section',undefined,'coverage-body glass'),sample=el('input'),language=el('input'),family=el('input');sample.value='美食外卖 超市便利 韩国炸鸡啤酒屋 Aa 0123 👨‍👩‍👧‍👦';sample.maxLength=512;sample.setAttribute('aria-label','检查文字');language.value='zh-Hans';language.setAttribute('aria-label','检查语言');family.value='sans-serif';family.setAttribute('aria-label','检查字体族');controls.append(el('h2','设备字体与覆盖检查'),sample,language,family);
  const output=el('div',undefined,'coverage-output');
  const buttons=[['字体清单',async()=>{const inv=await request({op:'font_inventory'});output.replaceChildren();output.append(el('p',`${inv.families.length} 个字体族 · ${inv.aliases.length} 个别名`));for(const f of inv.families){const row=el('details');row.append(el('summary',`${f.name||(f.languages||[]).join('、')||'通用补字'} · ${roleLabels[f.role]||f.role} · ${f.generated?'已生成':'系统'}`),el('p',`${f.config} · ${f.files.join('、')}`));output.append(row);}for(const a of inv.aliases)output.append(el('p',`${a.name} → ${a.resolved||a.target}${a.error?' · '+a.error:''}`));for(const e of inv.errors)output.append(el('p',e));}],['检查当前方案',async()=>{const r=await request({op:'coverage_report',profile:getProfile(),text:sample.value,language:language.value,role:family.value});output.replaceChildren(el('p','静态覆盖预测；未验证应用实际选字。'));for(const f of r.families)output.append(el('p',`${f.name} · ${f.state==='planned'?'计划接管':'系统'} · ${f.reason}`));for(const c of r.sample)output.append(el('span',`${c.character} ${c.codepoint} → ${c.source}　`));for(const w of r.warnings)output.append(el('p',w));}],['设备实际选字',async()=>{const r=await request({op:'glyph_probe',text:sample.value,language:language.value,role:family.value});output.replaceChildren(el('p',`${r.verification} · ${r.fontMapSource||'系统'} · Android 选字；应用内置引擎仍需单独验收。`));for(const g of r.glyphs)output.append(el('p',g.file?`${g.glyphId} → ${g.file.split('/').pop()} · ${g.weight}`:`${g.character||g.glyphId} · ${g.hasGlyph?'有字形':'待确认'}`));}]];
  for(const [label,fn]of buttons){const b=el('button',label,'glass-button');b.onclick=()=>run(fn);controls.append(b);}reportRoot.append(controls,output);
  const app=el('input');app.placeholder='应用包名，如 com.taobao.taobao';app.setAttribute('aria-label','应用包名');const inspect=el('button','检查应用字体来源','glass-button');inspect.onclick=()=>run(async()=>{const r=await request({op:'app_font_evidence',package:app.value.trim()});output.replaceChildren(el('p','仅表示进程加载了这些文件，不能证明具体文字命中它们。'));if(!r.processes.length)output.append(el('p','未找到可读取的运行进程，请先打开应用。'));for(const p of r.processes){output.append(el('strong',`${p.process} · ${p.pid}`),el('p',(p.engines||[]).join('、')));for(const f of p.mappedFonts||[])output.append(el('p',f));if(p.error)output.append(el('p',p.error));}});controls.append(app,inspect);
  refresh();return {refresh};
}

export function fallbackEditor({slot,fonts,rule,markDirty,render,openFont}) {
  const el=(tag,value,cls)=>{const e=document.createElement(tag);if(value)e.textContent=value;if(cls)e.className=cls;return e;};
  const root=el('details',undefined,'fallback-editor');root.append(el('summary','补字字体链'));
  root.append(el('p','按顺序补字，最后保留系统补字。补字字体可分别设置真实字重与斜体。','field-help'));
  for(const [i,f]of (slot.fallbacks||[]).entries()){const row=el('div',undefined,'coverage-rule');const id=f.assetId||f.faces?.[0]?.assetId;row.append(el('span',`${i+1}. ${fonts.find(a=>a.id===id)?.label||'补字字体'}`));for(const [label,fn]of [['编辑',()=>openFont(i)],['上移',()=>{if(i>0)[slot.fallbacks[i-1],slot.fallbacks[i]]=[slot.fallbacks[i],slot.fallbacks[i-1]];}],['移除',()=>slot.fallbacks.splice(i,1)]]){const b=el('button',label,'glass-button');b.type='button';b.onclick=()=>{fn();markDirty();render();};row.append(b);}root.append(row);}
  const select=el('select');select.setAttribute('aria-label','新增补字字体');const placeholder=el('option','选择补字字体');placeholder.value='';select.append(placeholder);
  for(const asset of fonts)for(const face of asset.faces){if(face.italic||!faceEligible(face,rule,true))continue;const o=el('option',`${asset.label} · ${face.name}`);o.value=`${asset.id}:${face.index}`;select.append(o);}
  select.onchange=()=>{if(!select.value)return;if((slot.fallbacks||[]).length>=8)return;const [id,index]=select.value.split(':');const face=fonts.find(a=>a.id===id).faces.find(f=>f.index===Number(index));(slot.fallbacks||=[]).push(makeFontSlot(id,face));markDirty();render();};root.append(select);return root;
}
