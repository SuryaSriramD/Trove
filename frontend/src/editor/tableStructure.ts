import type { block_table_v1 as Table } from "../../../contracts/generated/domain";
type Axis = "row" | "column";
const countKey = (axis: Axis) => axis === "row" ? "rowCount" : "columnCount";
const spanKey = (axis: Axis) => axis === "row" ? "rowSpan" : "colSpan";
const headerKey = (axis: Axis) => axis === "row" ? "headerRows" : "headerColumns";
export function tableWidths(table: Table): number[] {
  return Array.from({ length: table.columnCount }, (_, index) => table.columnWidths[index] || 180);
}
function shiftCell(cell: Table["cells"][number], axis: Axis, index: number, delta: number): Table["cells"][number] {
  if (cell[axis] < index) return cell;
  const shiftEvidence = (value: unknown): unknown => {
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Restore merged cells before changing this table's structure.");
    const original = value as Table["cells"][number];
    if (!Number.isInteger(original.row) || !Number.isInteger(original.column) || !Array.isArray(original.richText) || !Array.isArray(original.preMergeCells)) throw new Error("Original cell evidence is incomplete. Restore this merge before changing the structure.");
    return shiftCell(original, axis, index, delta);
  };
  return { ...cell, [axis]: cell[axis] + delta, preMergeCells: cell.preMergeCells.map(shiftEvidence) } as Table["cells"][number];
}
export function insertTableLine(table: Table, axis: Axis, index: number, ids: string[]): Table {
  const count = table[countKey(axis)], span = spanKey(axis), other = axis === "row" ? "column" : "row";
  if (!Number.isInteger(index) || index < 0 || index > count) throw new Error("Choose an existing row or column.");
  if (table.cells.some(cell => cell[axis] < index && cell[axis] + cell[span] > index)) throw new Error("Restore cells in the merged range before inserting through it.");
  const needed = table[countKey(other)];
  if (ids.length !== needed || new Set(ids).size !== ids.length) throw new Error("Could not allocate distinct table cell identities.");
  const cells = table.cells.map(cell => shiftCell(cell, axis, index, 1));
  for (let i = 0; i < needed; i++) cells.push({ id: ids[i], row: axis === "row" ? index : i, column: axis === "column" ? index : i, rowSpan: 1, colSpan: 1, richText: [], preMergeCells: [] });
  const widths = tableWidths(table);
  if (axis === "column") widths.splice(index, 0, widths[Math.max(0, index - 1)] ?? 180);
  return { ...table, [countKey(axis)]: count + 1, [headerKey(axis)]: table[headerKey(axis)] + (index < table[headerKey(axis)] ? 1 : 0), columnWidths: widths, cells };
}
export function tableLineRemoval(table: Table, axis: Axis, index: number) {
  if (table[countKey(axis)] <= 1) throw new Error(`A table needs at least one ${axis}.`);
  if (!Number.isInteger(index) || index < 0 || index >= table[countKey(axis)]) throw new Error("Choose an existing row or column.");
  const affected = table.cells.filter(cell => cell[axis] <= index && cell[axis] + cell[spanKey(axis)] > index);
  if (affected.some(cell => cell[spanKey(axis)] > 1)) throw new Error("Restore cells in the merged range before removing through it.");
  return affected.filter(cell => cell.richText.some(run => run.text.length > 0) || cell.preMergeCells.length > 0);
}
export function removeTableLine(table: Table, axis: Axis, index: number): Table {
  tableLineRemoval(table, axis, index);
  const widths = tableWidths(table);
  if (axis === "column") widths.splice(index, 1);
  return { ...table, [countKey(axis)]: table[countKey(axis)] - 1, [headerKey(axis)]: Math.max(0, table[headerKey(axis)] - (index < table[headerKey(axis)] ? 1 : 0)), columnWidths: widths,
    cells: table.cells.filter(cell => cell[axis] !== index).map(cell => shiftCell(cell, axis, index + 1, -1)) };
}
