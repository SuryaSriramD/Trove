import test from "node:test";
import assert from "node:assert/strict";
import {colorTokens,foreground,background,cssColorToken,colorToken} from "../src/editor/palette.ts";
import {richNodes,richText} from "../src/editor/codec.ts";
test("finite palettes accept exact supported CSS values and reject executable or unknown values",()=>{
 for(const token of colorTokens){assert.equal(cssColorToken(foreground[token]),token);assert.equal(cssColorToken(background[token],true),token);assert.equal(colorToken(token),token);}
 assert.equal(cssColorToken("rgb(255, 0, 0)"),"red");assert.equal(cssColorToken("#ff0",true),"yellow");
 for(const unknown of ["#123456","url(https://example.test)","expression(alert(1))","rgb(999,0,0)","red; background:url(x)"])assert.equal(cssColorToken(unknown),null);
});
test("rich colors and highlights stay attached to marked and linked text across line breaks",()=>{
 const value:any=[{text:"Keep\nlinked",marks:["bold"],link:"https://example.test",color:"purple",highlight:"yellow"}];
 const round=richText(richNodes(value));assert.equal(round.map(run=>run.text).join(""),value[0].text);
 for(const run of round){assert.equal(run.color,"purple");assert.equal(run.highlight,"yellow");assert.equal(run.link,value[0].link);assert.deepEqual(run.marks,["bold"]);}
});
