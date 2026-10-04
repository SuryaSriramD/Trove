import hljs from "highlight.js";
// Highlight once so multi-line comments/strings keep their language context,
// then balance only the trusted highlighter's spans around each visible line.
export function highlightedLines(source: string, language: string): string[] {
  const html = hljs.getLanguage(language) ? hljs.highlight(source,{language}).value : hljs.highlightAuto(source).value;
  const lines: string[] = [], opened: string[] = []; let current="";
  for (const token of html.split(/(<span\b[^>]*>|<\/span>|\n)/g)) {
    if(token==="\n") {lines.push(current+"</span>".repeat(opened.length));current=opened.join("");}
    else {current+=token;if(token.startsWith("<span"))opened.push(token);else if(token==="</span>")opened.pop();}
  }
  lines.push(current);
  return lines;
}
