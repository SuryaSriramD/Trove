import assert from 'node:assert/strict';
import test from 'node:test';
import {parseDeepLink} from '../src/deepLinks.ts';
const page='019a1234-5678-7000-8000-000000000002';
const block='019a1234-5678-7000-8000-000000000003';
const workspace='019a1234-5678-7000-8000-000000000001';
test('legacy page links and scoped copied block links retain exact identities',()=>{
  assert.deepEqual(parseDeepLink(`trove://page/${page}`),{pageId:page});
  assert.deepEqual(parseDeepLink(`trove://page/${page}#${block}`),{pageId:page,blockId:block});
  assert.deepEqual(parseDeepLink(`trove://page/${page}/block/${block}?workspace=${workspace}`),{pageId:page,blockId:block,workspaceId:workspace});
});
test('deep links cannot smuggle paths, credentials, fragments or ambiguous scopes',()=>{
  for(const value of [
    `https://page/${page}`,`trove://evil/${page}`,`trove://user@page/${page}`,
    `trove://page:80/${page}`,`trove://page/${page}/block/nope`,
    `trove://page/${page}?path=/private/data`,`trove://page/${page}#invalid`,
    `trove://page/${page}/block/${block}#${block}`,
    `trove://page/${page}?workspace=${workspace}&workspace=${workspace}`,
    `trove://page/${page}?workspace=`,`trove://page/${page}%2fblock%2f${block}`,
    'trove://page/'+ 'a'.repeat(600),
  ]) assert.throws(()=>parseDeepLink(value),value);
});
