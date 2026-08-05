import type { Node, NodeSortMode, PingResult } from "./api";

export function compareNodesByPing(a: Node, b: Node, pingMap: Map<string, PingResult>): number {
  const pa = pingMap.get(a.id);
  const pb = pingMap.get(b.id);
  if (!pa?.ok && !pb?.ok) return a.name.localeCompare(b.name, "ru");
  if (pa?.ok && !pb?.ok) return -1;
  if (!pa?.ok && pb?.ok) return 1;
  return (pa?.latencyMs ?? 9999) - (pb?.latencyMs ?? 9999);
}

export function compareNodesByFrequent(
  a: Node,
  b: Node,
  selectCounts: Record<string, number>,
): number {
  const ca = selectCounts[a.id] ?? 0;
  const cb = selectCounts[b.id] ?? 0;
  if (cb !== ca) return cb - ca;
  return a.name.localeCompare(b.name, "ru");
}

export function sortNodes(
  nodes: Node[],
  mode: NodeSortMode,
  pingMap: Map<string, PingResult>,
  selectCounts: Record<string, number>,
): Node[] {
  const sorted = [...nodes];
  switch (mode) {
    case "name-desc":
      sorted.sort((a, b) => b.name.localeCompare(a.name, "ru"));
      break;
    case "ping":
      sorted.sort((a, b) => compareNodesByPing(a, b, pingMap));
      break;
    case "frequent":
      sorted.sort((a, b) => compareNodesByFrequent(a, b, selectCounts));
      break;
    default:
      sorted.sort((a, b) => a.name.localeCompare(b.name, "ru"));
  }
  return sorted;
}

/** Выбранный узел всегда первым, остальное — по sortMode. */
export function orderNodes(
  nodes: Node[],
  selectedNodeId: string | undefined,
  sortMode: NodeSortMode,
  pingMap: Map<string, PingResult>,
  selectCounts: Record<string, number>,
): Node[] {
  if (!selectedNodeId) {
    return sortNodes(nodes, sortMode, pingMap, selectCounts);
  }
  const selected = nodes.find((n) => n.id === selectedNodeId);
  const rest = nodes.filter((n) => n.id !== selectedNodeId);
  const sortedRest = sortNodes(rest, sortMode, pingMap, selectCounts);
  return selected ? [selected, ...sortedRest] : sortedRest;
}
