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

export function createPreview({request,lookup,key,getSlot}) {
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
  let disposed=false,revision=0,serial=0,cacheBytes=0;
  const cache=new Map();
  const cancelled=()=>disposed;
  async function load(ref) {
    const id=`${ref.assetId}:${ref.faceIndex}`;
    if(cache.has(id))return cache.get(id).promise;
    const entry={face:null,size:0,promise:null};cache.set(id,entry);
    entry.promise=(async()=>{
      let session;
      try {
        if(cancelled() || !wanted.has(id))throw new Error('预览已取消');
        session=await request({op:'preview_prepare',id:ref.assetId,faceIndex:ref.faceIndex});
        if(cancelled() || !wanted.has(id))throw new Error('预览已取消');
        if(!Number.isSafeInteger(session.size)||session.size<12||session.size>256*1048576)throw new Error('预览大小无效');
        // Evict fonts not used by this draw before reserving the next buffer.
        for(const [k,e] of cache)if(!wanted.has(k)&&e.face){document.fonts.delete(e.face);cacheBytes-=e.size;cache.delete(k);}
        if(cacheBytes+session.size>384*1048576)throw new Error('预览字体占用过大，请收起九档对照后重试');
        entry.size=session.size;cacheBytes+=entry.size;
        const bytes=new Uint8Array(session.size);let offset=0;
        while(offset<bytes.length){
          if(cancelled() || !wanted.has(id))throw new Error('预览已取消');
          const r=await request({op:'preview_chunk',token:session.token,offset});
          const chunk=Uint8Array.from(atob(r.chunk),c=>c.charCodeAt(0));
          if(!chunk.length||chunk.length>48*1024||r.offset!==offset+chunk.length||r.size!==bytes.length||r.offset>bytes.length)throw new Error('字体预览传输不完整');
          bytes.set(chunk,offset);offset=r.offset;
        }
        const name=`hfc_preview_${key}_${Date.now()}_${++serial}`;
        const face=new FontFace(name,bytes.buffer,{weight:'400',style:ref.meta.italic?'italic':'normal'});
        await face.load();if(cancelled() || !wanted.has(id))throw new Error('预览已取消');
        document.fonts.add(face);entry.face=face;return name;
      }catch(e){cache.delete(id);cacheBytes-=entry.size;entry.size=0;throw e;}
      finally{if(session)try{await request({op:'preview_release',token:session.token});}catch{/* Expired previews are reclaimed by the engine. */}}
    })();
    return entry.promise;
  }
  let wanted=new Set();
  async function draw(){
    const version=++revision,slot=getSlot();if(disposed)return;
    const req=Number(select.value),styleValue=style.value;
    const rows=[{weight:req,target:main}];main.replaceChildren();comparison.replaceChildren();
    if(details.open)for(const weight of weights){const target=el('div',undefined,'preview-sample');comparison.append(target);rows.push({weight,target});}
    const resolved=rows.map(r=>({...r,ref:previewFace(slot,r.weight,styleValue,lookup)}));
    wanted=new Set(resolved.filter(r=>r.ref.assetId).map(r=>`${r.ref.assetId}:${r.ref.faceIndex}`));
    for(const [id,e] of cache)if(!wanted.has(id)&&e.face){document.fonts.delete(e.face);cacheBytes-=e.size;cache.delete(id);}
    for(const {weight,target,ref} of resolved){
      const label=el('small',`${weight} · ${ref.error||'正在加载…'}`),specimen=el('p',sample.value);
      specimen.hidden=true;target.append(label,specimen);
      if(ref.error)continue;
      if(ref.system){specimen.style.fontFamily=key==='serif'?'serif':key==='monospace'?'monospace':'sans-serif';specimen.style.fontWeight=weight;specimen.style.fontStyle=styleValue;label.textContent=`${weight} · 当前 WebView 系统字体`;specimen.hidden=false;continue;}
      try{
        const name=await load(ref);if(disposed||version!==revision)return;
        specimen.style.fontFamily=`"${name}"`;specimen.style.fontWeight='400';specimen.style.fontStyle=ref.meta.italic?'italic':'normal';specimen.style.fontSynthesis='none';
        if(ref.synthetic){specimen.style.fontStyle='oblique';specimen.style.fontSynthesis='style';}
        specimen.style.fontVariationSettings=ref.axes?Object.entries(ref.axes).map(([tag,v])=>`"${tag}" ${v}`).join(', '):'normal';
        label.textContent=`${weight} → ${ref.actual} · ${ref.meta.name}${ref.synthetic?' · 模拟倾斜':''}`;specimen.hidden=false;
      }catch(e){if(disposed||version!==revision)return;label.textContent=`${weight} · ${e.message}`;}
    }
  }
  // Scheduling coalesces slider events; file loading never mutates the profile.
  let frame;
  const refresh=()=>{cancelAnimationFrame(frame);frame=requestAnimationFrame(()=>void draw());};
  select.onchange=style.onchange=sample.oninput=details.ontoggle=refresh;
  return {root,key,refresh,dispose(){disposed=true;revision++;cancelAnimationFrame(frame);for(const e of cache.values())if(e.face)document.fonts.delete(e.face);cache.clear();}};
}
