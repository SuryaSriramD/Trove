import type { Block, block_table_v1 as Table, RichText } from '../../../contracts/generated/domain';
import type { NodeJSON } from './codec';
const plain = (value: RichText = []) => value.map(x => x.text).join('');

// Pre-merge cells are complete canonical snapshots, including nested merge history.
export function mergeCells(table: Table, first: string, last: string): Table {
  const a = table.cells.find(c => c.id === first), b = table.cells.find(c => c.id === last);
  if (!a || !b) throw new Error('Choose two table cells');
  const top = Math.min(a.row,b.row), left = Math.min(a.column,b.column);
  const bottom = Math.max(a.row+a.rowSpan,b.row+b.rowSpan), right = Math.max(a.column+a.colSpan,b.column+b.colSpan);
  const cells = table.cells.filter(c => c.row < bottom && c.row+c.rowSpan > top && c.column < right && c.column+c.colSpan > left);
  if (cells.length < 2 || cells.some(c => c.row < top || c.column < left || c.row+c.rowSpan > bottom || c.column+c.colSpan > right)) throw new Error('Select a complete rectangular range');
  if (cells.reduce((n,c) => n+c.rowSpan*c.colSpan,0) !== (bottom-top)*(right-left)) throw new Error('The range has missing cells');
  const originals = structuredClone(cells.sort((a,b) => a.row-b.row || a.column-b.column));
  const richText: RichText = originals.flatMap((c,i) => [...(i ? [{text:'\n'}] : []), ...c.richText]);
  return {...table,cells:[...table.cells.filter(c=>!cells.includes(c)),{...originals[0],row:top,column:left,rowSpan:bottom-top,colSpan:right-left,richText,preMergeCells:originals}]};
}
export function splitCell(table: Table, id: string): Table {
  const cell = table.cells.find(c=>c.id===id);
  if (!cell || !cell.preMergeCells.length) throw new Error('This imported merge has no original cells to restore');
  const originals = structuredClone(cell.preMergeCells) as Table['cells'];
  if (originals.some(c=>!c.id || !Array.isArray(c.richText))) throw new Error('Original cell evidence is incomplete');
  const merged = originals.flatMap((c,i)=>[...(i?[{text:'\n'}]:[]),...c.richText]);
  // Keep edits made after merging on the anchor cell, while restoring all other cells.
  if (JSON.stringify(merged)!==JSON.stringify(cell.richText)) originals[0].richText=structuredClone(cell.richText);
  return {...table,cells:[...table.cells.filter(c=>c.id!==id),...originals]};
}
export function safeWebURL(value: string): string | null {
  try { const u=new URL(value); return ['https:','http:'].includes(u.protocol) && !u.username && !u.password ? u.href : null; } catch { return null; }
}
export function inertEmbedDocument(text: string): string {
  const safe=text.replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;');
  return `<!doctype html><meta http-equiv="Content-Security-Policy" content="default-src 'none'; base-uri 'none'; form-action 'none'"><body><pre>${safe}</pre></body>`;
}
export function blockReadable(b: Block): string {
  const d=b.data as any;
  if (d.richText) return plain(d.richText);
  if (d.cells) return d.cells.map((c:any)=>plain(c.richText)).join('\n');
  if (d.tabs) return d.tabs.map((t:any)=>t.title).join('\n');
  return d.source ?? d.latex ?? d.readableText ?? d.offlineText ?? d.title ?? d.label ?? d.displaySnapshot ?? d.start ?? '';
}
// Replace only user-visible textual content; IDs, URLs, expressions and preserved
// raw/pre-merge evidence are deliberately not rewritten.
export function replaceDocument(doc: NodeJSON, find: string, replacement: string): {doc:NodeJSON; count:number} {
  const out=structuredClone(doc); let count=0;
  if (!find) return {doc:out,count};
  const replace=(s:string)=>s.split(find).map((part,i)=>{if(i)count++;return part}).join(replacement);
  // A visible match can span several marked runs, including inside a table.
  // Edit from the end so offsets remain stable; the first run supplies marks.
  const replaceRuns=<T,>(runs:T[],read:(run:T)=>string,write:(run:T,text:string)=>T):T[]=>{
    const text=runs.map(read).join('');
    const starts:number[]=[]; let index=0;
    while((index=text.indexOf(find,index))>=0){starts.push(index);index+=find.length;}
    for(const start of starts.reverse()){
      let at=0; const next:T[]=[]; let inserted=false;
      for(const run of runs){const text=read(run),end=at+text.length;
        if(end<=start || at>=start+find.length)next.push(run);
        else{
          const before=text.slice(0,Math.max(0,start-at)),after=text.slice(Math.max(0,start+find.length-at));
          if(before)next.push(write(run,before));
          if(!inserted){if(replacement)next.push(write(run,replacement));inserted=true;}
          if(after)next.push(write(run,after));
        }
        at=end;
      }
      runs=next;count++;
    }
    return runs;
  };
  for(const node of out.content??[]) {
    if(node.type==='troveText') {
      node.content=replaceRuns(node.content??[],n=>n.type==='hardBreak'?'\n':n.text??'',(n,text)=>({...n,type:'text',text}));
    } else {
      const d=node.attrs?.data;
      if(d?.cells) for(const c of d.cells) c.richText=replaceRuns<RichText[number]>(c.richText,r=>r.text,(r,text)=>({...r,text}));
      for(const key of ['title','description','offlineText','readableText','label','displaySnapshot']) if(typeof d?.[key]==='string')d[key]=replace(d[key]);
    }
  }
  return {doc:out,count};
}
