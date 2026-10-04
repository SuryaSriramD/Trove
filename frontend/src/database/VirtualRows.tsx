import {
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

// Variable-height rows preserve a focused editor while the viewport moves.
// Only loaded records participate: the surrounding pager owns fetching/limits.
export function VirtualRows({
  count,
  estimate = 80,
  identity,
  table,
  header,
  width,
  colSpan = 1,
  renderRow,
  recordId,
}: {
  count: number;
  estimate?: number;
  identity: string;
  table?: boolean;
  header?: ReactNode;
  width?: number;
  colSpan?: number;
  renderRow: (index: number) => ReactNode;
  recordId?: (index: number) => string;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const [scroll, setScroll] = useState(0),
    [height, setHeight] = useState(560),
    [focused, setFocused] = useState<number | null>(null);
  const [sizes, setSizes] = useState<Record<number, number>>({});
  useLayoutEffect(() => {
    setSizes({});
    setScroll(0);
    setFocused(null);
    if (viewport.current) viewport.current.scrollTop = 0;
  }, [identity]);
  useLayoutEffect(() => {
    const element = viewport.current;
    if (!element) return;
    const observer = new ResizeObserver(() => setHeight(element.clientHeight));
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const offsets = useMemo(() => {
    const values = [0];
    for (let i = 0; i < count; i++)
      values.push(values[i] + (sizes[i] ?? estimate));
    return values;
  }, [count, sizes, estimate]);
  const indices = useMemo(() => {
    const first = Math.max(
      0,
      offsets.findIndex((offset) => offset >= Math.max(0, scroll - 200)) - 1,
    );
    let last = first;
    while (last < count && offsets[last] < scroll + height + 200) last++;
    const selected = new Set(
      Array.from({ length: Math.max(0, last - first) }, (_, i) => first + i),
    );
    if (focused !== null && focused < count) selected.add(focused);
    return [...selected].sort((a, b) => a - b);
  }, [offsets, scroll, height, count, focused]);
  useLayoutEffect(() => {
    const element = viewport.current;
    if (!element) return;
    const observer = new ResizeObserver((entries) =>
      setSizes((old) => {
        const next = { ...old };
        let changed = false;
        for (const entry of entries) {
          const index = Number(
            (entry.target as HTMLElement).dataset.windowIndex,
          );
          const value = Math.ceil(entry.target.getBoundingClientRect().height);
          if (value > 0 && old[index] !== value) {
            next[index] = value;
            changed = true;
          }
        }
        return changed ? next : old;
      }),
    );
    element
      .querySelectorAll("[data-window-index]")
      .forEach((row) => observer.observe(row));
    return () => observer.disconnect();
  }, [indices.join(","), identity]);
  const children: ReactNode[] = [];
  let position = 0;
  const spacer = (end: number) => {
    const size = offsets[end] - offsets[position];
    if (size > 0)
      children.push(
        table ? (
          <tr key={`gap-${position}`} aria-hidden="true">
            <td
              colSpan={colSpan}
              style={{ height: size, padding: 0, border: 0 }}
            />
          </tr>
        ) : (
          <div
            key={`gap-${position}`}
            aria-hidden="true"
            style={{ height: size }}
          />
        ),
      );
  };
  for (const index of indices) {
    spacer(index);
    children.push(
      table ? (
        <tr
          key={index}
          data-window-index={index}
          data-record-id={recordId?.(index)}
          aria-rowindex={index + 2}
        >
          {renderRow(index)}
        </tr>
      ) : (
        <div key={index} data-window-index={index}>
          {renderRow(index)}
        </div>
      ),
    );
    position = index + 1;
  }
  spacer(count);
  return (
    <div
      ref={viewport}
      className="db-virtual-viewport"
      onScroll={(event) => setScroll(event.currentTarget.scrollTop)}
      onFocusCapture={(event) => {
        const row = (event.target as HTMLElement).closest<HTMLElement>(
          "[data-window-index]",
        );
        if (row) setFocused(Number(row.dataset.windowIndex));
      }}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node))
          setFocused(null);
      }}
    >
      {table ? (
        <table
          className="db-table"
          aria-rowcount={count + 1}
          style={{ tableLayout: "fixed", width }}
        >
          {header}
          <tbody>{children}</tbody>
        </table>
      ) : (
        children
      )}
    </div>
  );
}
