import test from "node:test";
import assert from "node:assert/strict";
import {highlightedLines} from "../src/editor/codePresentation.ts";
test("line rendering retains multi-line syntax spans without interpreting source markup",()=>{
 const source='/* Multi\ncomment */\nconst text = "<script>🌱";\n';
 const lines=highlightedLines(source,"javascript");
 assert.equal(lines.length,4);assert.match(lines[0],/hljs-comment/);assert.match(lines[1],/hljs-comment/);
 assert(!lines.join("").includes("<script>"));assert(lines.join("").includes("&lt;script&gt;"));
 for(const line of lines) assert.equal((line.match(/<span\b/g)??[]).length,(line.match(/<\/span>/g)??[]).length);
 assert.equal(source,'/* Multi\ncomment */\nconst text = "<script>🌱";\n');
});
