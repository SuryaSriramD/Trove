import { createHighlighterCore } from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";
import { bundledLanguages } from "shiki/langs";
export type SyntaxToken = { content:string; color?:string; darkColor?:string; fontStyle?:number };
const highlighter = createHighlighterCore({
  themes:[import("shiki/themes/github-light.mjs"),import("shiki/themes/github-dark.mjs")],
  langs:[],engine:createJavaScriptRegexEngine({target:"ES2018"}),
});
const loads = new Map<string,Promise<void>>();
const aliases:Record<string,string>={js:"javascript",ts:"typescript",py:"python",sh:"shellscript",bash:"shellscript",yml:"yaml",md:"markdown","c++":"cpp","c#":"csharp"};
export function plainCodeLines(source:string):SyntaxToken[][] { return source.split(/\r?\n/).map(content=>[{content}]); }
export async function highlightedLines(source:string,language:string):Promise<SyntaxToken[][]> {
  const requested=language.trim().toLowerCase(),lang=aliases[requested]??requested;
  if(!(lang in bundledLanguages))return plainCodeLines(source);
  const renderer=await highlighter;
  let loading=loads.get(lang);
  if(!loading){loading=renderer.loadLanguage(bundledLanguages[lang as keyof typeof bundledLanguages]);loads.set(lang,loading);}
  await loading;
  return renderer.codeToTokensWithThemes(source,{lang,themes:{light:"github-light",dark:"github-dark"}}).map(line=>line.map(token=>({content:token.content,color:token.variants.light.color,darkColor:token.variants.dark.color,fontStyle:token.variants.light.fontStyle})));
}
