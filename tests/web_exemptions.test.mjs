import test from 'node:test';
import assert from 'node:assert/strict';
import {toggleExemptPackage,filterApps} from '../module/webroot/exemptions.js';

test('exemption multiselect preserves font configuration and empty default',()=>{
  const profile={slots:{chinese:{mode:'system'}}};
  toggleExemptPackage(profile,'com.sample.one',true);
  toggleExemptPackage(profile,'com.sample.two',true);
  toggleExemptPackage(profile,'com.sample.one',true);
  assert.deepEqual(profile.exemptPackages,['com.sample.one','com.sample.two']);
  toggleExemptPackage(profile,'com.sample.one',false);
  toggleExemptPackage(profile,'com.sample.two',false);
  assert.equal('exemptPackages' in profile,false);
  assert.deepEqual(profile.slots,{chinese:{mode:'system'}});
});
test('application search supports labels and packages',()=>{
  const apps=[{label:'淘宝',package:'com.taobao.taobao'},{label:'Settings',package:'com.android.settings'}];
  assert.equal(filterApps(apps,'淘宝')[0].package,'com.taobao.taobao');
  assert.equal(filterApps(apps,' ANDROID ')[0].label,'Settings');
});
test('limit failure does not mutate profile',()=>{
  const profile={exemptPackages:Array.from({length:128},(_,i)=>`com.app.p${i}`)};
  assert.throws(()=>toggleExemptPackage(profile,'com.app.extra',true));
  assert.equal(profile.exemptPackages.length,128);
});
