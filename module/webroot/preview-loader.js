const budget=128*1048576,hardLimit=384*1048576,expiry=2*60*1000;

// One bounded cache per WebUI session. Widgets can be disposed without forcing
// another transfer when the same face is opened in another category or style.
export function createPreviewLoader({request,fetchFont=globalThis.fetch?.bind(globalThis),now=Date.now}) {
  const cache=new Map();let bytes=0,serial=0,sweep;
  const remove=(id,entry)=>{if(entry.face)document.fonts.delete(entry.face);bytes-=entry.size;cache.delete(id);};
  function trim(required=0){
    for(const [id,e] of cache)if(!e.users.size && e.face && now()-e.used>=expiry)remove(id,e);
    for(const [id,e] of [...cache].sort((a,b)=>a[1].used-b[1].used)){
      if(bytes+required<=budget && cache.size<12)break;
      if(!e.users.size && e.face)remove(id,e);
    }
  }
  const schedule=()=>{clearTimeout(sweep);sweep=setTimeout(()=>trim(),expiry+1);sweep.unref?.();};
  function retain(owner,ids){
    for(const [id,e] of cache){if(ids.has(id))e.users.add(owner);else if(e.users.delete(owner))e.used=now();}
    trim();schedule();
  }
  function get(id){const e=cache.get(id);if(e?.face){e.used=now();return e.name;}return null;}
  function load(ref,owner){
    const id=`${ref.assetId}:${ref.faceIndex}`;
    if(cache.has(id)){const e=cache.get(id);e.users.add(owner);e.used=now();return e.promise;}
    const entry={face:null,name:'',size:0,used:now(),users:new Set([owner]),promise:null};cache.set(id,entry);
    entry.promise=(async()=>{
      let session;
      const check=()=>{if(!entry.users.size)throw new Error('预览已取消');};
      try {
        check();session=await request({op:'preview_prepare',id:ref.assetId,faceIndex:ref.faceIndex});check();
        if(!Number.isSafeInteger(session.size)||session.size<12||session.size>256*1048576)throw new Error('预览大小无效');
        trim(session.size);
        if(bytes+session.size>hardLimit)throw new Error('预览字体占用过大，请收起九档对照后重试');
        bytes+=session.size;entry.size=session.size;
        let buffer;
        if(fetchFont && /^preview-cache\/[a-f0-9]{64}-\d{1,2}\.ttf$/.test(session.url||'')){
          const controller=new AbortController();const timeout=setTimeout(()=>controller.abort(),15000);
          try {
            // Relative URL stays within the existing KSU/APatch WebView asset
            // loader. No network server or user-supplied absolute path is used.
            const response=await fetchFont(session.url,{signal:controller.signal});
            if(!response.ok)throw new Error('字体文件通道不可用');
            buffer=await response.arrayBuffer();
            if(buffer.byteLength!==session.size)throw new Error('字体文件传输不完整');
          }catch{buffer=null;}finally{clearTimeout(timeout);}
        }
        check();
        if(!buffer){
          const data=new Uint8Array(session.size);let offset=0;
          while(offset<data.length){
            check();const r=await request({op:'preview_chunk',token:session.token,offset});
            const binary=atob(r.chunk);
            if(!binary.length||binary.length>48*1024||r.offset!==offset+binary.length||r.size!==data.length||r.offset>data.length)throw new Error('字体预览传输不完整');
            for(let i=0;i<binary.length;i++)data[offset+i]=binary.charCodeAt(i);
            offset=r.offset;
          }
          buffer=data.buffer;
        }
        check();const name=`hfc_preview_${Date.now()}_${++serial}`;
        const face=new FontFace(name,buffer,{weight:'400',style:ref.meta.italic?'italic':'normal'});
        await face.load();check();document.fonts.add(face);entry.face=face;entry.name=name;entry.used=now();return name;
      }catch(e){remove(id,entry);throw e;}
      finally{if(session)request({op:'preview_release',token:session.token}).catch(()=>{});schedule();}
    })();
    return entry.promise;
  }
  return {get,load,retain,trim,dispose(){clearTimeout(sweep);for(const [id,e] of cache){e.users.clear();if(e.face)remove(id,e);}}};
}

let shared;
export function sharedPreviewLoader(request){return shared ||= createPreviewLoader({request});}
