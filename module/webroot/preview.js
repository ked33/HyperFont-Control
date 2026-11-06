import { sharedPreviewLoader } from './preview-loader.js';
const weights = Array.from({length:9}, (_, i) => (i+1)*100);

// Resolve explicitly: CSS matching must not silently choose another static file.
export function previewFace(slot, requested, style, lookup) {
  if (slot.mode === 'system') return {system:true, weight:requested, style};
  let ref, synthetic = false, actual = requested;
  if (slot.mode === 'variable') {
    const italic = style === 'italic' && slot.italicAssetId;
    ref = {assetId:italic ? slot.italicAssetId : slot.assetId, faceIndex:italic ? slot.italicFaceIndex || 0 : slot.faceIndex || 0};
    synthetic = style === 'italic' && !italic;
    const meta = lookup(ref.assetId, ref.faceIndex), axis = meta?.axes.find(a => a.tag === 'wght');
    if (!axis) return {error:'请先选择字体'};
    actual = slot.familyMode === 'instances' ? slot.weightMap?.[requested] : Math.max(axis.min, Math.min(axis.max,requested));
    if (!Number.isFinite(actual) || actual < axis.min || actual > axis.max) return {error:'请填写有效的轴值'};
    const axes = Object.fromEntries(meta.axes.filter(a=>a.tag!=='wght').map(a=>[a.tag,slot.axes?.[a.tag] ?? a.default]));
    if (Object.entries(axes).some(([tag,v])=>{const a=meta.axes.find(a=>a.tag===tag);return !Number.isFinite(v)||v<a.min||v>a.max;})) return {error:'其他可变轴值无效'};
    return {...ref,meta,actual,synthetic,style,axes:{...axes,wght:actual}};
  }
  let list = (slot.faces || []).filter(f=>f.style===style);
  if (!list.length && style === 'italic') { list=(slot.faces || []).filter(f=>f.style==='normal'); synthetic=true; }
  ref = list.find(f=>f.weight===requested);
  if (!ref && slot.missingWeightPolicy === 'nearest') ref=[...list].sort((a,b)=>Math.abs(a.weight-requested)-Math.abs(b.weight-requested)||a.weight-b.weight)[0];
  if (!ref) return {error:'此档未配置字体'};
  const meta=lookup(ref.assetId,ref.faceIndex);
  if (!meta) return {error:'字体已不在字体库'};
  return {...ref,meta,style,synthetic,actual:ref.weight};
}

export function createPreview({request,lookup,key,getSlot,loader=sharedPreviewLoader(request)}) {
  const el=(tag,value,cls)=>{const e=document.createElement(tag);if(value!==undefined)e.textContent=value;if(cls)e.className=cls;return e;};
  const root=el('section',undefined,'font-preview'), controls=el('div',undefined,'preview-controls');
  root.setAttribute('aria-label','字体实时预览');
  const title=el('strong','字体预览'), select=el('select'), style=el('select');
  select.setAttribute('aria-label','预览请求字重'); style.setAttribute('aria-label','预览正斜体');
  weights.forEach(w=>{const o=el('option',w);o.value=w;select.append(o);});select.value='400';
  [['normal','正体'],['italic','斜体']].forEach(([v,n])=>{const o=el('option',n);o.value=v;style.append(o);});
  const sample=el('input');sample.type='text';sample.maxLength=200;sample.setAttribute('aria-label','预览文字');
  sample.value=key==='chinese'?'中文字体 永和九年，岁在癸丑。':'The quick brown fox · Aa Ag 0123456789';
  const main=el('div',undefined,'preview-sample'), details=el('details'), summary=el('summary','展开九档对照'), comparison=el('div',undefined,'preview-comparison');
  details.append(summary,comparison);controls.append(title,select,style);root.append(controls,sample,main,details,el('p','字体文件预览；系统与应用效果请在提交并重启后验收。','field-help'));
  let disposed=false,revision=0;
  const owner={};
  const rowParts=new Map();
  const parts=target=>{
    if(!rowParts.has(target)){const label=el('small'),specimen=el('p');specimen.hidden=true;target.append(label,specimen);rowParts.set(target,{label,specimen});}
    return rowParts.get(target);
  };
  const comparisonRows=[];
  function paint(specimen,label,weight,ref,name){
    specimen.textContent=sample.value;
    specimen.style.fontFamily=`"${name}"`;specimen.style.fontWeight='400';specimen.style.fontStyle=ref.meta.italic?'italic':'normal';specimen.style.fontSynthesis='none';
    if(ref.synthetic){specimen.style.fontStyle='oblique';specimen.style.fontSynthesis='style';}
    specimen.style.fontVariationSettings=ref.axes?Object.entries(ref.axes).map(([tag,v])=>`"${tag}" ${v}`).join(', '):'normal';
    label.textContent=`${weight} → ${ref.actual} · ${ref.meta.name}${ref.synthetic?' · 模拟倾斜':''}`;specimen.hidden=false;
  }
  let wanted=new Set();
  async function draw(version){
    if(disposed || version!==revision)return;
    const slot=getSlot();
    const req=Number(select.value),styleValue=style.value;
    const rows=[{weight:req,target:main}];
    if(details.open){
      if(!comparisonRows.length)for(const weight of weights){const target=el('div',undefined,'preview-sample');comparison.append(target);comparisonRows.push({weight,target});}
      rows.push(...comparisonRows);
    }
    const resolved=rows.map(r=>({...r,ref:previewFace(slot,r.weight,styleValue,lookup)}));
    wanted=new Set(resolved.filter(r=>r.ref.assetId).map(r=>`${r.ref.assetId}:${r.ref.faceIndex}`));
    loader.retain(owner,wanted);
    const pending=[];
    for(const {weight,target,ref} of resolved){
      const {label,specimen}=parts(target);
      if(ref.error){label.textContent=`${weight} · ${ref.error}`;specimen.hidden=true;continue;}
      if(ref.system){specimen.textContent=sample.value;specimen.style.fontVariationSettings='normal';specimen.style.fontFamily=key==='serif'?'serif':key==='monospace'?'monospace':'sans-serif';specimen.style.fontWeight=weight;specimen.style.fontStyle=styleValue;specimen.style.fontSynthesis='';label.textContent=`${weight} · 当前 WebView 系统字体`;specimen.hidden=false;continue;}
      const cached=loader.get(`${ref.assetId}:${ref.faceIndex}`);
      if(cached){paint(specimen,label,weight,ref,cached);continue;}
      label.textContent=`${weight} · 正在加载…`;specimen.hidden=true;
      // Paint cached rows immediately. New faces may finish independently; the
      // revision guard prevents an old request from overwriting a newer choice.
      pending.push(loader.load(ref,owner).then(name=>{
        if(!disposed && version===revision)paint(specimen,label,weight,ref,name);
      }).catch(e=>{if(!disposed && version===revision)label.textContent=`${weight} · ${e.message}`;}));
    }
    await Promise.all(pending);
  }

  // Scheduling coalesces slider events; file loading never mutates the profile.
  let frame;
  const refresh=()=>{
    if(disposed)return;
    // Invalidate pending renders immediately, before the next animation frame.
    const version=++revision;
    cancelAnimationFrame(frame);frame=requestAnimationFrame(()=>void draw(version));
  };
  const selectWeight=(weight,requestedStyle)=>{
    if(disposed || !weights.includes(weight))return;
    select.value=String(weight);
    // Variable mapping edits preserve the manually selected style; static edits
    // follow the edited normal/italic column. Never move focus or scroll position.
    if(requestedStyle==='normal' || requestedStyle==='italic')style.value=requestedStyle;
    refresh();
  };
  select.onchange=style.onchange=sample.oninput=details.ontoggle=refresh;
  return {root,key,refresh,selectWeight,dispose(){disposed=true;revision++;cancelAnimationFrame(frame);loader.retain(owner,new Set());}};
}
