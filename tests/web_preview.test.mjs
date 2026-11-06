import test from 'node:test';
import assert from 'node:assert/strict';
import {previewFace} from '../module/webroot/preview.js';
import {adjustWeight,resetWeightMap,changeWeightMode} from '../module/webroot/weights.js';
const axis={tag:'wght',min:125,max:875,default:400};
const lookup=(id,index)=>id?{name:id,index,italic:id==='italic',axes:id==='static'?[]:[axis,{tag:'wdth',min:50,max:150,default:100}]}:null;
test('individual changes preserve fractions, clamp to actual range and never snap to slider steps',()=>{
 assert.equal(adjustWeight(412.5,50,axis),462.5);assert.equal(adjustWeight(850,50,axis),875);assert.equal(adjustWeight(130,-50,axis),125);assert.ok(Number.isNaN(adjustWeight(NaN,50,axis)));
});
test('reset drops retained values across a continuous-mode round trip',()=>{
 const defaults=()=>Object.fromEntries(Array.from({length:9},(_,i)=>[(i+1)*100,Math.max(125,Math.min(875,(i+1)*100))]));
 const s={familyMode:'instances',weightMap:{400:600},savedWeightMap:{400:700}};resetWeightMap(s,defaults);changeWeightMode(s,'continuous',defaults);changeWeightMode(s,'instances',defaults);assert.equal(s.weightMap[400],400);assert.equal(s.weightMap[100],125);assert.equal(s.weightMap[900],875);
});
test('variable preview uses mapping, selected collection face, italic and other axes',()=>{
 const s={mode:'variable',assetId:'variable',faceIndex:2,italicAssetId:'italic',italicFaceIndex:3,familyMode:'instances',weightMap:{400:550},axes:{wdth:110}};
 const r=previewFace(s,400,'italic',lookup);assert.equal(r.faceIndex,3);assert.equal(r.assetId,'italic');assert.deepEqual(r.axes,{wght:550,wdth:110});assert.equal(r.synthetic,false);
 delete s.italicAssetId;assert.equal(previewFace(s,400,'italic',lookup).synthetic,true);
 s.familyMode='continuous';assert.equal(previewFace(s,900,'normal',lookup).actual,875);
});
test('static preview resolves nearest lighter tie and never silently fills strict gaps',()=>{
 const s={mode:'static',missingWeightPolicy:'nearest',faces:[{assetId:'static',faceIndex:2,weight:300,style:'normal'},{assetId:'static',faceIndex:4,weight:500,style:'normal'}]};
 const r=previewFace(s,400,'normal',lookup);assert.equal(r.faceIndex,2);assert.equal(r.actual,300);assert.equal(previewFace(s,400,'italic',lookup).synthetic,true);
 s.missingWeightPolicy='reject';assert.match(previewFace(s,400,'normal',lookup).error,/未配置/);
});
test('invalid axes report an error rather than rendering a misleading fallback',()=>{
 const s={mode:'variable',assetId:'variable',familyMode:'instances',weightMap:{400:NaN}};assert.ok(previewFace(s,400,'normal',lookup).error);
 s.weightMap[400]=500;s.axes={wdth:200};assert.ok(previewFace(s,400,'normal',lookup).error);
 assert.equal(previewFace({mode:'system'},400,'normal',lookup).system,true);
});
