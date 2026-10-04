import {
  batch,
  dispatch,
  envelope,
  ids,
  pageGuards,
  snapshot,
  type WorkspaceSnapshot,
  type D,
  type S,
} from "../api";
export const guard = (
  entityType: S.EntityRef["entityType"],
  entityId: string,
  revision: string,
): S.ExpectedRevision => ({ target: { entityType, entityId }, revision });
export function uniqueGuards(guards: S.ExpectedRevision[]) {
  return [
    ...new Map(
      guards.map((g) => [`${g.target.entityType}:${g.target.entityId}`, g]),
    ).values(),
  ];
}
export async function ensureTasks(w: WorkspaceSnapshot): Promise<void> {
  if (w.collections.some((c) => c.systemRole === "tasks")) return;
  const [
    pageId,
    collectionId,
    titlePropertyId,
    statusPropertyId,
    duePropertyId,
    defaultOptionId,
    doneOptionId,
  ] = await ids(w, 7);
  await batch(
    w,
    [
      {
        kind: "tasks.ensure",
        pageId,
        collectionId,
        titlePropertyId,
        statusPropertyId,
        duePropertyId,
        defaultOptionId,
        doneOptionId,
      },
    ],
    [
      guard("page", pageId, "0"),
      guard("collection", collectionId, "0"),
      ...[titlePropertyId, statusPropertyId, duePropertyId].map((id) =>
        guard("property", id, "0"),
      ),
    ],
  );
}
export async function createTask(
  w: WorkspaceSnapshot,
  title: string,
): Promise<{ recordId: string; pageId: string }> {
  await ensureTasks(w);
  const current = await snapshot();
  const collection = current.collections.find((c) => c.systemRole === "tasks");
  if (!collection) throw Error("Tasks collection is unavailable.");
  const property = current.properties.find(
    (p) => p.collectionId === collection.id && p.type === "status",
  );
  if (!property) throw Error("Tasks status is unavailable.");
  const [recordId, pageId] = await ids(current, 2);
  await batch(
    current,
    [{ kind: "task.create", recordId, pageId, title }],
    [
      guard("record", recordId, "0"),
      guard("page", pageId, "0"),
      guard("collection", collection.id, collection.revision),
      guard("property", property.id, property.revision),
    ],
  );
  return { recordId, pageId };
}
export async function setTaskDone(
  w: WorkspaceSnapshot,
  recordId: string,
  done: boolean,
): Promise<void> {
  const record = w.records.find((r) => r.id === recordId),
    facet = w.taskFacets.find((f) => f.recordId === recordId),
    page = w.pages.find((p) => p.id === record?.pageId),
    property = w.properties.find((p) => p.id === facet?.statusPropertyId);
  if (!record || !facet || !page || !property)
    throw Error("Task is unavailable.");
  const config = property.config as {
    doneOptionId: string;
    reopenOptionId: string;
  };
  await batch(
    w,
    [
      {
        kind: "task.setStatus",
        recordId,
        statusOptionId: done ? config.doneOptionId : config.reopenOptionId,
      },
    ],
    uniqueGuards([
      ...pageGuards(w, page),
      guard("property", property.id, property.revision),
    ]),
  );
}
export function relationGuards(
  w: WorkspaceSnapshot,
  record: D.Record,
  property: D.PropertyDefinition,
  value: D.RawValue,
): S.ExpectedRevision[] {
  if (property.type !== "relation") return [];
  const config = property.config as { bidirectionalPropertyId?: string | null };
  const targets = [
    ...new Set([
      ...((value as string[]) ?? []),
      ...((record.values.find((v) => v.propertyId === property.id)
        ?.value as string[]) ?? []),
    ]),
  ];
  const result: S.ExpectedRevision[] = targets.map((id) => {
    const r = w.records.find((r) => r.id === id);
    if (!r) throw Error("Related record is unavailable");
    return guard("record", id, r.revision);
  });
  if (config.bidirectionalPropertyId) {
    const back = w.properties.find(
      (p) => p.id === config.bidirectionalPropertyId,
    );
    if (!back) throw Error("Reciprocal property is unavailable");
    result.push(guard("property", back.id, back.revision));
    for (const id of targets) {
      const r = w.records.find((r) => r.id === id)!;
      const p = w.pages.find((p) => p.id === r.pageId);
      if (!p) throw Error("Related page is unavailable");
      result.push(...pageGuards(w, p));
    }
  }
  return uniqueGuards(result);
}
export async function setProperty(
  w: WorkspaceSnapshot,
  record: D.Record,
  property: D.PropertyDefinition,
  value: D.RawValue,
) {
  const page = w.pages.find((p) => p.id === record.pageId);
  if (!page) throw Error("Record body is unavailable.");
  return batch(
    w,
    [
      {
        kind: "record.setValue",
        recordId: record.id,
        propertyId: property.id,
        value: {
          type: property.type,
          schemaVersion: property.schemaVersion,
          value,
        },
      },
    ],
    uniqueGuards([
      ...pageGuards(w, page),
      guard("property", property.id, property.revision),
      ...relationGuards(w, record, property, value),
    ]),
  );
}
export async function createRecord(
  w: WorkspaceSnapshot,
  collection: D.Collection,
  title: string,
) {
  const [recordId, pageId] = await ids(w, 2);
  await batch(
    w,
    [
      {
        kind: "record.create",
        recordId,
        pageId,
        collectionId: collection.id,
        title,
      },
    ],
    [
      guard("record", recordId, "0"),
      guard("page", pageId, "0"),
      guard("collection", collection.id, collection.revision),
    ],
  );
  return { recordId, pageId };
}
export async function saveView(
  w: WorkspaceSnapshot,
  view: Omit<D.View, "revision"> & { revision?: string },
) {
  const page = w.pages.find((p) => p.id === view.owningPageId),
    collection = w.collections.find((c) => c.id === view.collectionId),
    old = w.views.find((v) => v.id === view.id),
    oldPage = w.pages.find((p) => p.id === old?.owningPageId);
  if (!page || !collection) throw Error("View destination is unavailable.");
  const guards = [
    ...pageGuards(w, page),
    guard("collection", collection.id, collection.revision),
    guard("view", view.id, old?.revision ?? "0"),
  ];
  if (oldPage) guards.push(...pageGuards(w, oldPage));
  if (view.containerId) {
    const container = w.containers.find((c) => c.id === view.containerId);
    if (!container) throw Error("View container is unavailable.");
    guards.push(guard("container", container.id, container.revision));
  }
  const { id, revision, containerId, ...rest } = view;
  return batch(
    w,
    [
      {
        kind: "view.save",
        viewId: id,
        ...rest,
        ...(containerId ? { containerId } : {}),
      },
    ],
    uniqueGuards(guards),
  );
}
export function defaultQuery(properties: string[]): D.ViewQuery {
  return {
    filter: null,
    sorts: [],
    visiblePropertyIds: properties,
    groupByPropertyId: null,
    subgroupByPropertyId: null,
  };
}
export function viewConfig(
  type: string,
  properties: D.PropertyDefinition[],
): D.RawValue {
  if (type === "table") return { columnWidths: [] };
  if (type === "list") return { showPageIcon: true };
  if (type === "gallery") return { coverPropertyId: null, cardSize: "medium" };
  if (type === "board") {
    const property = properties.find(
      (p) =>
        p.type === "status" || p.type === "select" || p.type === "checkbox",
    );
    if (!property)
      throw Error("Add a status, select, or checkbox property for a board.");
    return { groupPropertyId: property.id, hideEmptyGroups: false };
  }
  const date = properties.find((p) => p.type === "date");
  if (!date) throw Error("Add a date property for a calendar.");
  if (type === "timeline") return { datePropertyId: date.id, scale: "month" };
  return { datePropertyId: date.id, weekStartsOn: 1 };
}
export async function createDatabase(w: WorkspaceSnapshot, title: string) {
  const [
    pageId,
    collectionId,
    titlePropertyId,
    statusId,
    dateId,
    open,
    doing,
    done,
    ...viewIds
  ] = await ids(w, 12);
  const query = defaultQuery([titlePropertyId, statusId, dateId]);
  const status = {
    options: [
      { id: open, label: "To do", group: "not_started" },
      { id: doing, label: "In progress", group: "in_progress" },
      { id: done, label: "Done", group: "done" },
    ],
    defaultOptionId: open,
    doneOptionId: done,
    reopenOptionId: open,
  };
  const configs: D.RawValue[] = [
    { columnWidths: [] },
    { showPageIcon: true },
    { groupPropertyId: statusId, hideEmptyGroups: false },
    { datePropertyId: dateId, weekStartsOn: 1 },
  ];
  const commands: S.Command[] = [
    { kind: "page.create", pageId, title },
    {
      kind: "collection.create",
      collectionId,
      owningPageId: pageId,
      titlePropertyId,
      title,
    },
    {
      kind: "collection.addProperty",
      collectionId,
      propertyId: statusId,
      name: "Status",
      type: "status",
      schemaVersion: 1,
      config: status,
    },
    {
      kind: "collection.addProperty",
      collectionId,
      propertyId: dateId,
      name: "Date",
      type: "date",
      schemaVersion: 1,
      config: {},
    },
  ];
  (["table", "list", "board", "calendar"] as const).forEach((type, i) =>
    commands.push({
      kind: "view.save",
      viewId: viewIds[i],
      collectionId,
      owningPageId: pageId,
      name: type[0].toUpperCase() + type.slice(1),
      type,
      schemaVersion: 1,
      config: configs[i],
      query,
    }),
  );
  await batch(w, commands, [
    guard("page", pageId, "0"),
    guard("collection", collectionId, "0"),
    ...[titlePropertyId, statusId, dateId].map((id) =>
      guard("property", id, "0"),
    ),
    ...viewIds.map((id) => guard("view", id, "0")),
  ]);
  return { collectionId, pageId };
}
export async function loadRecords(
  w: WorkspaceSnapshot,
  collectionId: string,
  viewId?: string,
): Promise<D.Record[]> {
  const records: D.Record[] = [];
  let cursor: string | undefined;
  do {
    const result = await dispatch<S.QueryRecordsResult>("queryRecords", {
      ...envelope(w),
      collectionId,
      ...(viewId ? { viewId } : {}),
      limit: 200,
      ...(cursor ? { cursor } : {}),
    });
    records.push(...result.records);
    cursor = result.nextCursor;
  } while (cursor);
  return records;
}

export async function clearProperty(
  w: WorkspaceSnapshot,
  record: D.Record,
  property: D.PropertyDefinition,
) {
  const page = w.pages.find((p) => p.id === record.pageId);
  if (!page) throw Error("Record body is unavailable.");
  return batch(
    w,
    [
      {
        kind: "record.clearValue",
        recordId: record.id,
        propertyId: property.id,
      },
    ],
    uniqueGuards([
      ...pageGuards(w, page),
      guard("property", property.id, property.revision),
      ...relationGuards(w, record, property, []),
    ]),
  );
}
