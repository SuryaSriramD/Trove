import test from "node:test";
import assert from "node:assert/strict";
import { presentationBlocks, blockVisibility } from "../src/editor/presentation.ts";
const node = (id: string, kind: string, depth: number, collapsed = false) => ({ type: "troveText", attrs: { blockId: id, kind, depth, data: { collapsed } } });
const fixture = [node("outer", "toggle", 0, true), node("child", "bulleted_list_item", 1), node("inner", "toggle_heading", 2, true), node("grandchild", "paragraph", 3), node("code", "code", 3), node("sibling", "paragraph", 0)];
test("collapsed ancestry hides grandchildren and technical blocks without hiding siblings", () => {
  assert.deepEqual([...blockVisibility(presentationBlocks(fixture), new Set()).hidden], ["child", "inner", "grandchild", "code"]);
});
test("reopening a parent respects a nested toggle's own collapse state", () => {
  const opened = structuredClone(fixture); opened[0].attrs.data.collapsed = false;
  assert.deepEqual([...blockVisibility(presentationBlocks(opened), new Set()).hidden], ["grandchild", "code"]);
});
test("citation reveal opens only presentation state and leaves canonical values unchanged", () => {
  const original = JSON.stringify(fixture);
  const shown = blockVisibility(presentationBlocks(fixture), new Set(["grandchild"]));
  assert.deepEqual([...shown.hidden], []);
  assert(shown.expanded.has("outer") && shown.expanded.has("inner"));
  assert.equal(JSON.stringify(fixture), original);
});
test("layout proxies hide the complete source subtree and a cited source can be revealed", () => {
  const layout = [node("columns", "columns", 0), node("nested", "toggle", 1), node("source", "paragraph", 2), node("other", "paragraph", 1)];
  assert.deepEqual([...blockVisibility(presentationBlocks(layout), new Set()).hidden], ["nested", "source", "other"]);
  assert.deepEqual([...blockVisibility(presentationBlocks(layout), new Set(["source"])).hidden], ["other"]);
});
test("unsaved indentation changes and draft identities determine current visibility", () => {
  const moved = structuredClone(fixture); moved[3].attrs.depth = 0;
  assert(!blockVisibility(presentationBlocks(moved), new Set()).hidden.has("grandchild"));
  const draft = [{type:"troveText",attrs:{clientKey:"draft-toggle",kind:"toggle",depth:0,data:{collapsed:true}}},{type:"troveText",attrs:{clientKey:"draft-child",kind:"paragraph",depth:1}}];
  assert.deepEqual([...blockVisibility(presentationBlocks(draft), new Set()).hidden], ["draft-child"]);
});
