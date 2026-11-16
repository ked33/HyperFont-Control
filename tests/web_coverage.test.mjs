import test from 'node:test';
import assert from 'node:assert/strict';
import {editorEntries,findRule,getSlot,putSlot,faceEligible,makeFontSlot} from '../module/webroot/coverage.js';

test('advanced editing targets its rule and preserves the four core slots',()=>{
  const p={slots:{chinese:{mode:'system'}},coverage:{rules:[{id:'han-serif',role:'serif',scripts:['Hani'],font:{mode:'system'}}]}};
  assert.equal(editorEntries(p,[['chinese','中文']]).length,2);
  const slot={mode:'static',faces:[{assetId:'test',weight:400}],fallbacks:[{mode:'static',faces:[]}]};
  putSlot(p,'rule:han-serif',slot);
  assert.equal(getSlot(p,'rule:han-serif'),slot);
  assert.equal(findRule(p,'rule:han-serif').role,'serif');
  assert.deepEqual(p.slots.chinese,{mode:'system'});
});

test('special scopes stay separate and a weightless variable font uses default instance',()=>{
  const face={index:0,weight:400,italic:false,scripts:['Hani'],axes:[{tag:'wdth',min:80,max:120}],privateUse:true,emoji:true,colorFormats:['COLR']};
  assert.equal(faceEligible(face,{role:'emoji'}),true);
  assert.equal(faceEligible({...face,colorFormats:[]},{role:'emoji'}),false);
  assert.equal(faceEligible(face,{role:'sans',scripts:['Arab']}),false);
  assert.equal(makeFontSlot('test',face).mode,'static');
  assert.equal(makeFontSlot('test',{...face,axes:[{tag:'wght',min:300,max:700}]}).weightMap[900],700);
});
