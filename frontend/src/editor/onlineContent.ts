import { safeWebURL } from './advanced.ts';
export function matchingBookmarkCache(url:string,cachedSourceUrl?:string):boolean {return !!cachedSourceUrl&&safeWebURL(url)!==null&&safeWebURL(url)===safeWebURL(cachedSourceUrl);}
export type OnlineEmbed = {provider:string;url:string;origin:string};
export function onlineEmbed(raw:string):OnlineEmbed|null {
 try{const input=new URL(raw);if(input.protocol!=='https:'||input.username||input.password||input.port)return null;const host=input.hostname.toLowerCase();let id:string|undefined;
  if(['youtube.com','www.youtube.com','m.youtube.com','youtube-nocookie.com','www.youtube-nocookie.com','youtu.be'].includes(host)){
   id=host==='youtu.be'?input.pathname.slice(1):input.pathname==='/watch'?input.searchParams.get('v')??undefined:/^\/(?:embed|shorts)\/([^/]+)$/.exec(input.pathname)?.[1];
   if(!id||!/^[-\w]{11}$/.test(id))return null;
   const url=new URL(`https://www.youtube-nocookie.com/embed/${id}`);url.searchParams.set('autoplay','0');url.searchParams.set('playsinline','1');url.searchParams.set('enablejsapi','0');return {provider:'YouTube',url:url.href,origin:url.origin};
  }
  if(['vimeo.com','www.vimeo.com','player.vimeo.com'].includes(host)){
   id=/^\/(?:video\/)?([0-9]{1,12})$/.exec(input.pathname)?.[1];if(!id)return null;const url=new URL(`https://player.vimeo.com/video/${id}`);url.searchParams.set('autoplay','0');url.searchParams.set('dnt','1');const hash=input.searchParams.get('h');if(hash&&/^[a-f0-9]{1,32}$/i.test(hash))url.searchParams.set('h',hash);return {provider:'Vimeo',url:url.href,origin:url.origin};
  }
 }catch{}return null;
}
// Imported HTML is never an input to a live frame. The parent document's CSP
// allows only these two origins, and the frame keeps normal cross-origin access
// checks while scripts/storage remain confined to the provider's own origin.
export const onlineEmbedSandbox='allow-scripts allow-same-origin allow-presentation';
