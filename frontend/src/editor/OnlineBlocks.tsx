import { useEffect, useRef, useState } from 'react';
import { dispatch, envelope, type D, type S, type WorkspaceSnapshot } from '../api';
import { inertEmbedDocument, safeWebURL } from './advanced';
import { matchingBookmarkCache, onlineEmbed, onlineEmbedSandbox } from './onlineContent';
export function BookmarkBlock({data,update,workspace,pageId,readOnly}:{data:D.block_bookmark_v1;update:(patch:Partial<D.block_bookmark_v1>)=>void;workspace:WorkspaceSnapshot|null;pageId?:string;readOnly:boolean}) {
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),latest=useRef({data,update,readOnly});latest.current={data,update,readOnly};
 const matches=matchingBookmarkCache(data.url,data.cachedSourceUrl),stale=!!data.fetchedAt&&!matches;
 const fetchMetadata=async()=>{if(!workspace||!pageId||readOnly||busy)return;const requested=data.url;setBusy(true);setError('');try{
  const result=await dispatch<S.BookmarkPreviewResult>('editor.bookmarkPreview',{...envelope(workspace),pageId,url:requested,consent:'fetch_once'} satisfies S.BookmarkPreviewRequest);
  const current=latest.current;if(current.readOnly)return;if(current.data.url!==requested||result.sourceUrl!==requested){setError('The URL changed during the request. Saved metadata was kept.');return;}
  current.update({title:result.title,description:result.description,cachedSourceUrl:result.sourceUrl,fetchedAt:result.fetchedAt});
 }catch{setError('Metadata is unavailable. Your saved content is unchanged.');}finally{setBusy(false);}};
 return <div className="bookmark-card"><a href={safeWebURL(data.url)??undefined} target="_blank" rel="noopener noreferrer">{stale?data.url:data.title||data.url}</a>{!stale&&<p>{data.description}</p>}
  {matches&&data.fetchedAt&&<small>Cached metadata · {new Date(data.fetchedAt).toLocaleString()} · available offline</small>}
  {stale&&<details><summary>Saved metadata belongs to a previous URL</summary><p>{data.title}</p><p>{data.description}</p><span>{data.cachedSourceUrl}</span></details>}
  {!readOnly&&<div className="bookmark-fetch"><p>Fetch metadata once contacts this HTTPS website. Only its title and description are saved; images and scripts are not loaded.</p><button type="button" disabled={busy||!workspace||!pageId||!data.url.startsWith('https:')} onClick={()=>void fetchMetadata()}>{busy?'Fetching metadata…':'Fetch metadata once'}</button></div>}
  {error&&<p role="alert">{error}</p>}<details><summary>Edit bookmark</summary><fieldset disabled={readOnly||busy}><label>Title<input aria-label="Bookmark title" value={data.title} onChange={event=>update({title:event.target.value})}/></label><label>URL<input aria-label="Bookmark URL" value={data.url} onChange={event=>update({url:event.target.value})}/></label><label>Description<textarea aria-label="Bookmark description" value={data.description??''} onChange={event=>update({description:event.target.value})}/></label></fieldset></details>
 </div>;
}
export function EmbedBlock({data,update,readOnly}:{data:D.block_embed_v1;update:(patch:Partial<D.block_embed_v1>)=>void;readOnly:boolean}) {
 const target=onlineEmbed(data.url),[enabled,setEnabled]=useState<string|null>(null),[offline,setOffline]=useState(!navigator.onLine);
 const consentKey=`${data.provider}\n${data.url}`;
 useEffect(()=>{setEnabled(null);},[data.url,data.provider]);
 useEffect(()=>{const onOffline=()=>{setOffline(true);setEnabled(null);},onOnline=()=>setOffline(false);window.addEventListener('offline',onOffline);window.addEventListener('online',onOnline);return()=>{window.removeEventListener('offline',onOffline);window.removeEventListener('online',onOnline);};},[]);
 const live=!!target&&enabled===consentKey&&!offline;
 return <div className="embed-card">
  {live&&target?<><iframe key={`online:${target.url}`} title={`${target.provider} enabled online embed`} sandbox={onlineEmbedSandbox} allow="fullscreen; picture-in-picture" referrerPolicy="strict-origin-when-cross-origin" src={target.url}/><button type="button" onClick={()=>setEnabled(null)}>Stop online embed</button><p>The provider controls its player. If unavailable, use the saved text below or open the source.</p></>:<><iframe key="offline" title={`${data.provider} offline embed`} sandbox="" referrerPolicy="no-referrer" srcDoc={inertEmbedDocument(data.offlineText)}/><p>{offline?'Offline · saved text is available.':'Saved text · the online player is off.'}</p></>}
  {target&&!live&&!offline&&<div><p>Enable {target.provider} for this view to contact its servers and run its player in a sandbox. This permission ends when you close the view or change the URL.</p><button type="button" onClick={()=>setEnabled(consentKey)}>Enable {target.provider} for this view</button></div>}
  {!target&&<p>This source has no supported live player. Saved text and the external link remain available.</p>}
  {live&&<div className="embed-offline-fallback"><strong>Saved offline text</strong><p>{data.offlineText}</p></div>}
  <a href={safeWebURL(data.url)??undefined} target="_blank" rel="noopener noreferrer">Open source externally</a>
  <details><summary>Edit embed</summary><fieldset disabled={readOnly}><label>Source URL<input aria-label="Embed source URL" value={data.url} onChange={event=>update({url:event.target.value,provider:safeWebURL(event.target.value)?new URL(event.target.value).hostname:data.provider})}/></label><label>Offline content<textarea aria-label="Embed offline text" value={data.offlineText} onChange={event=>update({offlineText:event.target.value})}/></label></fieldset></details>
 </div>;
}
