import type { D } from "../api";
export type P2Config = {
  chart: {
    groupPropertyId: string | null;
    valuePropertyId: string | null;
    aggregation: "count" | "sum" | "average" | "min" | "max";
    visualization: "bar" | "donut";
  };
  form: { enabled: boolean; requiredPropertyIds: string[] };
  buttons: P2Button[];
};
export type P2Button = {
  id: string;
  label: string;
  propertyId: string;
  value: { type: string; schemaVersion: number; value: D.RawValue };
  confirmationRequired: boolean;
};
export type P2Chart = {
  collectionId: string;
  revision: string;
  config: P2Config["chart"];
  recordCount: number;
  representation: string;
  groups: {
    key: string;
    label: string;
    decimal: string | null;
    exactFraction: string | null;
    recordIds: string[];
    numericCount: number;
  }[];
};
export const p2FieldTypes = [
  "text",
  "number",
  "select",
  "multi_select",
  "status",
  "checkbox",
  "date",
  "url",
  "email",
  "phone",
];
// Normalize with integer arithmetic before converting the small ratio to SVG
// coordinates. Large canonical decimals never pass through a JS Number.
export function chartRatios(
  values: (string | null)[],
  mode: "bar" | "donut",
): number[] {
  const decimals = values.map((v) => v ?? "0");
  if (decimals.some((v) => !/^-?\d+(\.\d+)?$/.test(v)))
    throw Error("Invalid chart decimal");
  const scale = Math.max(
    0,
    ...decimals.map((v) => (v.split(".")[1] ?? "").length),
  );
  const integers = decimals.map((v) => {
    const [whole, fraction = ""] = v.split(".");
    return BigInt(`${whole}${fraction.padEnd(scale, "0")}`);
  });
  if (mode === "donut" && integers.some((v) => v < 0n))
    throw Error(
      "Donut charts require nonnegative values. Use bars for signed values.",
    );
  const abs = (n: bigint) => (n < 0n ? -n : n);
  const denominator =
    mode === "donut"
      ? integers.reduce((a, b) => a + b, 0n)
      : integers.reduce((a, b) => (abs(b) > a ? abs(b) : a), 0n);
  return integers.map((v) =>
    denominator === 0n ? 0 : Number((v * 1000000n) / denominator) / 1000000,
  );
}
export function formFieldEmpty(value: D.RawValue): boolean {
  if (value == null) return true;
  if (typeof value === "string") return !value.trim();
  if (Array.isArray(value))
    return (
      value.length === 0 ||
      value.every(
        (v) =>
          typeof v === "object" &&
          v != null &&
          "text" in v &&
          !String(v.text).trim(),
      )
    );
  return false;
}
