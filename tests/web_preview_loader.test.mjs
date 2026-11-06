import test from 'node:test';
import assert from 'node:assert/strict';
import {createPreviewLoader} from '../module/webroot/preview-loader.js';

test('direct transport avoids chunk calls and reuses faces across widgets',async t=>{
  const oldDocument=globalThis.document,oldFace=globalThis.FontFace;
  globalThis.document={fonts:new Set()};globalThis.FontFace=class {async load(){return this;}};
  let loader; t.after(()=>{loader?.dispose();globalThis.document=oldDocument;globalThis.FontFace=oldFace;});
  const calls=[];let fetches=0,time=0;
  const id='a'.repeat(64),ref={assetId:id,faceIndex:0,meta:{italic:false}};
  loader=createPreviewLoader({now:()=>time,request:async q=>{calls.push(q.op);if(q.op==='preview_prepare')return {token:'token',size:20*1048576,url:`preview-cache/${id}-0.ttf`};if(q.op==='preview_chunk')throw new Error('chunk path used');return {};},fetchFont:async()=>{fetches++;return {ok:true,arrayBuffer:async()=>new ArrayBuffer(20*1048576)};}});

  const first={},second={};const name=await loader.load(ref,first);
  assert.equal(fetches,1);assert.equal(calls.filter(x=>x==='preview_prepare').length,1);assert.equal(calls.filter(x=>x==='preview_chunk').length,0);
  loader.retain(first,new Set());assert.equal(loader.get(`${id}:0`),name);
  assert.equal(await loader.load(ref,second),name);assert.equal(fetches,1);
  loader.retain(second,new Set());time=120001;loader.trim();assert.equal(loader.get(`${id}:0`),null);assert.equal(document.fonts.size,0);
});

test('unavailable direct file channel falls back with bounds checks',async t=>{
  const oldDocument=globalThis.document,oldFace=globalThis.FontFace;
  globalThis.document={fonts:new Set()};globalThis.FontFace=class {async load(){return this;}};
  let loader; t.after(()=>{loader?.dispose();globalThis.document=oldDocument;globalThis.FontFace=oldFace;});
  let chunks=0;const id='b'.repeat(64),size=96*1024;
  loader=createPreviewLoader({request:async q=>{
    if(q.op==='preview_prepare')return {token:'token',size,url:`preview-cache/${id}-0.ttf`};
    if(q.op==='preview_chunk'){chunks++;return {chunk:Buffer.alloc(48*1024).toString('base64'),offset:q.offset+48*1024,size};}
    return {};
  },fetchFont:async()=>({ok:false})});
  await loader.load({assetId:id,faceIndex:0,meta:{italic:false}},{});assert.equal(chunks,2);
});
