import test from 'node:test';
import assert from 'node:assert/strict';
import {createPreview} from '../module/webroot/preview.js';
import {createPreviewLoader} from '../module/webroot/preview-loader.js';

// Minimal DOM to exercise the real preview controller without browser font APIs.
class Element {
  constructor(tag) { this.tagName=tag; this.children=[]; this.style={}; this.attributes={}; }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.children=children; }
  setAttribute(name,value) { this.attributes[name]=value; }
}

test('preview follows edited steps, preserves manual choices and coalesces rapid updates', async t=>{
  const originals=Object.fromEntries(['document','requestAnimationFrame','cancelAnimationFrame','FontFace'].map(k=>[k,globalThis[k]]));
  t.after(()=>{for(const [k,v] of Object.entries(originals))if(v===undefined)delete globalThis[k];else globalThis[k]=v;});
  const frames=new Map();let nextFrame=0,loads=0;
  globalThis.document={createElement:tag=>new Element(tag),fonts:new Set()};
  globalThis.requestAnimationFrame=fn=>{frames.set(++nextFrame,fn);return nextFrame;};
  globalThis.cancelAnimationFrame=id=>frames.delete(id);
  globalThis.FontFace=class { async load(){return this;} };
  let slot={mode:'variable',assetId:'font',faceIndex:0,familyMode:'instances',weightMap:{400:400,500:550,700:775}};
  const meta={name:'Test',italic:false,axes:[{tag:'wght',min:100,max:900,default:400}]};
  const request=async q=>{
    if(q.op==='preview_prepare'){loads++;return {token:'test',size:12};}
    if(q.op==='preview_chunk')return {chunk:Buffer.alloc(12).toString('base64'),offset:12,size:12};
    return {};
  };
  const loader=createPreviewLoader({request});t.after(()=>loader.dispose());
  const widget=createPreview({key:'chinese',getSlot:()=>slot,lookup:()=>meta,request,loader});
  const [controls,,main,details]=widget.root.children;
  const [,weight,style]=controls.children;
  style.value='normal';
  const flush=async()=>{const batch=[...frames.values()];frames.clear();for(const fn of batch)fn();await new Promise(resolve=>setImmediate(resolve));};
  widget.refresh();await flush();assert.match(main.children[0].textContent,/400 → 400/);
  const originalSpecimen=main.children[1];
  style.value='italic';style.onchange();await flush();
  widget.selectWeight(700);assert.equal(weight.value,'700');assert.equal(style.value,'italic');await flush();
  assert.match(main.children[0].textContent,/700 → 775/);
  assert.equal(main.children[1].style.fontVariationSettings,'"wght" 775');
  assert.match(main.children[0].textContent,/模拟倾斜/);
  assert.equal(loads,1,'adjusting a mapped weight reuses the loaded font');
  assert.equal(main.children[1],originalSpecimen,'axis updates reuse the specimen node');

  // Global adjustment/default refreshes keep the chosen request and style.
  slot.weightMap[700]=700;widget.refresh();await flush();
  assert.equal(weight.value,'700');assert.match(main.children[0].textContent,/700 → 700/);
  widget.selectWeight(500);widget.selectWeight(700);widget.selectWeight(500);
  assert.equal(frames.size,1);await flush();assert.match(main.children[0].textContent,/500 → 550/);
  weight.value='400';weight.onchange();await flush();assert.match(main.children[0].textContent,/400 → 400/);

  // Static rows explicitly follow the edited style column, including empty rows.
  slot={mode:'static',missingWeightPolicy:'reject',faces:[{assetId:'font',faceIndex:0,weight:500,style:'normal'}]};
  widget.selectWeight(500,'normal');await flush();assert.equal(style.value,'normal');assert.match(main.children[0].textContent,/500 → 500/);
  widget.selectWeight(700,'italic');await flush();assert.equal(style.value,'italic');assert.match(main.children[0].textContent,/700 · 此档未配置字体/);
  widget.selectWeight(123,'normal');assert.equal(weight.value,'700');assert.equal(style.value,'italic');
  widget.dispose();widget.selectWeight(400);assert.equal(frames.size,0);
  loader.dispose();assert.equal(document.fonts.size,0);
});
