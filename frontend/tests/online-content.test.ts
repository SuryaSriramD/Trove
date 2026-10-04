import {test} from 'node:test';import assert from 'node:assert/strict';
import {matchingBookmarkCache,onlineEmbed,onlineEmbedSandbox} from '../src/editor/onlineContent.ts';
test('bookmark cache belongs to its normalized source URL',()=>{assert.equal(matchingBookmarkCache('https://example.com/','https://example.com'),true);assert.equal(matchingBookmarkCache('https://changed.example','https://example.com'),false);assert.equal(matchingBookmarkCache('javascript:alert(1)','javascript:alert(1)'),false);});
test('live players accept exact supported HTTPS provider paths and discard untrusted URL parameters',()=>{
 const youtube=onlineEmbed('https://www.youtube.com/watch?v=M7lc1UVf-VE&autoplay=1&origin=https://attacker.test')!;assert.equal(youtube.url,'https://www.youtube-nocookie.com/embed/M7lc1UVf-VE?autoplay=0&playsinline=1&enablejsapi=0');
 const vimeo=onlineEmbed('https://vimeo.com/76979871')!;assert.equal(vimeo.url,'https://player.vimeo.com/video/76979871?autoplay=0&dnt=1');
 for(const url of ['http://youtu.be/M7lc1UVf-VE','https://youtube.com.attacker.test/watch?v=M7lc1UVf-VE','https://user@youtube.com/watch?v=M7lc1UVf-VE','https://youtube.com:8443/watch?v=M7lc1UVf-VE','javascript:alert(1)','data:text/html,<script>alert(1)</script>','https://example.com','https://youtube.com/embed/%22%3E%3Cscript%3E'])assert.equal(onlineEmbed(url),null,url);
 assert.equal(onlineEmbedSandbox,'allow-scripts allow-same-origin allow-presentation');assert.ok(!onlineEmbedSandbox.includes('allow-top-navigation'));assert.ok(!onlineEmbedSandbox.includes('allow-popups'));assert.ok(!onlineEmbedSandbox.includes('allow-forms'));
});
