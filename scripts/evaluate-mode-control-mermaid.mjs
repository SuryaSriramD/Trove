#!/usr/bin/env node
// Evaluation only. Parse with the product's pinned Mermaid implementation;
// never render, execute links, repair a draft, or replace a model output.
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { createHash } from 'node:crypto';

const args = Object.fromEntries(Array.from({ length: (process.argv.length - 2) / 2 }, (_, i) => [process.argv[2 + i * 2], process.argv[3 + i * 2]]));
if (!args['--root'] || !['golden', 'outputs'].includes(args['--mode'])) throw new Error('Specify --root and --mode golden|outputs');
const root = fs.realpathSync(args['--root']);
const raw = fs.readFileSync(path.join(root, 'internal/workspace/testdata/expanded-evaluation/independent-mode-controls-v1.json'));
const corpusSHA256 = createHash('sha256').update(raw).digest('hex');
if (corpusSHA256 !== '9d23ba556d0cec8a7b54ab91fdf9039cc55d8931791d3023158ee6d23f15eed1') throw new Error('Sealed corpus changed');
const cases = JSON.parse(raw).cases.filter(c => c.mode === 'mermaid');
if (cases.length !== 4) throw new Error('Exact four diagram controls required');
const packageRoot = fs.realpathSync(path.join(root, 'frontend/node_modules/mermaid'));
const version = JSON.parse(fs.readFileSync(path.join(packageRoot, 'package.json'), 'utf8')).version;
if (version !== '12.0.0' || JSON.parse(fs.readFileSync(path.join(root, 'frontend/package.json'), 'utf8')).dependencies.mermaid !== version) throw new Error('Actual parser does not match product pin');
const entry = path.join(packageRoot, 'dist/mermaid.core.mjs');
const { default: mermaid } = await import(pathToFileURL(entry).href);
const initialize = () => mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true, logLevel: 'fatal', flowchart: { htmlLabels: false } });
const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const sorted = list => list.map(item => JSON.stringify(item)).sort();
const empty = value => value == null || (value instanceof Map || value instanceof Set ? value.size === 0 : Array.isArray(value) ? value.length === 0 : Object.keys(value).length === 0);
function requireValue(value, message) { if (!value) throw new Error(message); }

async function evaluate(control, source) {
  requireValue(typeof source === 'string' && source.trim() && Buffer.byteLength(source) <= 32000, 'Diagram source missing or out of bounds');
  requireValue(!source.includes('```') && !source.includes('%%') && !/^\s*---/m.test(source), 'Unrequested fences, comments/directives or frontmatter');
  initialize();
  const diagram = await mermaid.mermaidAPI.getDiagramFromText(source);
  const db = diagram.db;
  const expected = control.diagram;
  requireValue(!db.getDiagramTitle?.() && !db.getAccTitle?.() && !db.getAccDescription?.(), 'Unrequested diagram metadata');
  if (expected.kind === 'flowchart') {
    requireValue(['flowchart', 'flowchart-v2'].includes(diagram.type), 'Wrong diagram kind');
    const direction = db.getDirection();
    requireValue((direction === 'TB' ? 'TD' : direction) === expected.direction, 'Wrong graph direction');
    const vertices = db.getVertices();
    requireValue(vertices instanceof Map && same([...vertices.keys()].sort(), [...expected.nodes].sort()), 'Case-sensitive node set differs');
    for (const [id, node] of vertices) {
      requireValue(node.id === id && (node.text == null || node.text === id) && !node.type && !node.link && !node.haveCallback && !node.icon && !node.img && !node.dir && empty(node.styles) && empty(node.classes) && empty(node.props), 'Unrequested node label/shape/style/link/metadata');
    }
    requireValue(empty(db.getSubGraphs()) && empty(db.getClasses()), 'Unrequested subgraphs or styles');
    const rawEdges = db.getEdges();
    requireValue(empty(rawEdges.defaultStyle) && !rawEdges.defaultInterpolate, 'Unrequested default edge styling');
    const edges = rawEdges.map(edge => {
      requireValue(edge.type === 'arrow_point' && edge.stroke === 'normal' && !edge.text && !edge.isUserDefinedId && !edge.animate && !edge.animation && empty(edge.style) && empty(edge.classes), 'Edge arrow/label/style differs');
      return { from: edge.start, to: edge.end, kind: '-->', label: '' };
    });
    requireValue(same(sorted(edges), sorted(expected.edges)), 'Directed edge multiset differs');
    return { kind: 'flowchart', parserType: diagram.type, direction, nodes: [...vertices.keys()], edges };
  }
  requireValue(expected.kind === 'sequenceDiagram' && diagram.type === 'sequence', 'Wrong sequence diagram kind');
  requireValue(same(db.getActorKeys(), expected.participants), 'Participant identity/order differs');
  const actors = db.getActors();
  requireValue(actors instanceof Map && actors.size === expected.participants.length, 'Participant inventory differs');
  for (const [id, actor] of actors) {
    requireValue(actor.name === id && actor.description === id && actor.type === 'participant' && !actor.box && empty(actor.links) && empty(actor.properties), 'Unrequested participant alias/type/link/metadata');
  }
  requireValue(empty(db.getBoxes()) && empty(db.getCreatedActors()) && empty(db.getDestroyedActors()) && !db.showSequenceNumbers(), 'Unrequested sequence effects');
  const messages = db.getMessages().map(message => {
    requireValue(!message.activate && !message.centralConnection && (message.type === db.LINETYPE.SOLID || message.type === db.LINETYPE.DOTTED), 'Unexpected message kind, activation or sequence directive');
    requireValue(typeof message.message === 'string', 'Non-text message metadata');
    return { from: message.from, to: message.to, kind: message.type === db.LINETYPE.SOLID ? '->>' : '-->>', label: message.message };
  });
  requireValue(same(messages, expected.messages), 'Message order/direction/arrow/label differs');
  return { kind: 'sequenceDiagram', parserType: diagram.type, participants: db.getActorKeys(), messages };
}

const results = [];
async function record(control, label, source, expected) {
  let projection = null, error = null;
  try { projection = await evaluate(control, source); } catch (failure) { error = String(failure?.message ?? failure); }
  const accepted = error === null;
  results.push({ fixture: control.id, label, expected, accepted, passed: accepted === expected, source, projection, error });
}
if (args['--mode'] === 'golden') {
  // Golden parser failure is a harness/environment blocker. It is never
  // recorded as a malformed native model result, and no model runs follow it.
  for (const control of cases) {
    await record(control, 'golden', control.golden, true);
    for (const counterexample of control.counterexamples) {
      if (counterexample.mustReject !== true) throw new Error('Unlocked diagram counterexample');
      await record(control, counterexample.kind, counterexample.output, false);
    }
  }
} else {
  if (!args['--input']) throw new Error('Outputs mode requires --input reports JSON');
  const reports = JSON.parse(fs.readFileSync(args['--input'], 'utf8'));
  for (const control of cases) {
    requireValue(reports[control.id]?.fixture === control.id, 'Missing exact native diagram report');
    await record(control, 'native_displayed', reports[control.id].displayed, true);
  }
}
const passed = results.every(result => result.passed);
console.log(JSON.stringify({ schemaVersion: 1, corpusSHA256, parserVersion: version, parserEntry: entry, parserEntrySHA256: createHash('sha256').update(fs.readFileSync(entry)).digest('hex'), nodeVersion: process.version, mode: args['--mode'], passed, goldenEnvironmentOrHarnessBlocked: args['--mode'] === 'golden' && results.some(result => result.label === 'golden' && !result.passed), results, releaseQualified: false }, null, 2));
process.exitCode = passed ? 0 : 1;
