import type { ColorToken } from "../../../contracts/generated/domain";
export const colorTokens: ColorToken[] = ["gray","brown","orange","yellow","green","blue","purple","pink","red"];
export const foreground: Record<ColorToken,string> = {gray:"#60646c",brown:"#8c5a3c",orange:"#b75d00",yellow:"#856900",green:"#287442",blue:"#2161a6",purple:"#7642a8",pink:"#ab3a78",red:"#b73737"};
export const background: Record<ColorToken,string> = {gray:"#e9e9eb",brown:"#efdfd1",orange:"#ffe2c0",yellow:"#fff2ae",green:"#d9eedc",blue:"#dceafa",purple:"#eadff7",pink:"#f8dfee",red:"#f9dcdc"};
export function colorToken(value: unknown): ColorToken | null {
  return typeof value === "string" && colorTokens.includes(value as ColorToken) ? value as ColorToken : null;
}
const normalized = (value: string) => {
  const s=value.trim().toLowerCase().replace(/\s/g,"");
  const rgb=/^rgb\((\d+),(\d+),(\d+)\)$/.exec(s);
  if(rgb&&rgb.slice(1).every(v=>Number(v)<=255)) return "#"+rgb.slice(1).map(v=>Number(v).toString(16).padStart(2,"0")).join("");
  return /^#[\da-f]{3}$/.test(s)?"#"+s.slice(1).split("").map(c=>c+c).join(""):s;
};
export function cssColorToken(value: string, highlight=false): ColorToken | null {
  const s=normalized(value), token=colorToken(s==="grey"?"gray":s);
  if(token) return token;
  const standard:Record<string,ColorToken>={"#808080":"gray","#a52a2a":"brown","#ffa500":"orange","#ffff00":"yellow","#008000":"green","#0000ff":"blue","#800080":"purple","#ffc0cb":"pink","#ff0000":"red"};
  return (Object.entries(highlight?background:foreground).find(([,css])=>css===s)?.[0] as ColorToken|undefined)??standard[s]??null;
}
