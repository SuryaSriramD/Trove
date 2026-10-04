import test from "node:test";
import assert from "node:assert/strict";
import {highlightedLines} from "../src/editor/codePresentation.ts";
test("Shiki highlights multi-line syntax with theme tokens without changing source text",async()=>{
 const source='/* Multi\ncomment */\nconst text = "<script>🌱";\n';
 const lines=await highlightedLines(source,"javascript");
 assert.equal(lines.length,4);assert.equal(lines.map(line=>line.map(token=>token.content).join("")).join("\n"),source);
 assert.equal(lines[0][0].color,lines[1][0].color);assert(lines[0][0].darkColor);
 assert(lines[2].some(token=>token.content.includes("<script>")));
});
test("unknown imported code languages remain readable plain text without guessed syntax",async()=>{
 const source="retained opaque\nbytes";const lines=await highlightedLines(source,"unknown-future-language");
 assert.equal(lines.map(line=>line.map(token=>token.content).join("")).join("\n"),source);assert(!lines[0][0].color);
});
