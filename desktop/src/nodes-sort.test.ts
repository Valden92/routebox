import { describe, expect, it } from "vitest";
import type { Node, PingResult } from "./api";
import { orderNodes, sortNodes } from "./nodes-sort";

const nodes: Node[] = [
  { id: "b", name: "Bravo", protocol: "vless", host: "b.example", port: 443 },
  { id: "a", name: "Alpha", protocol: "vless", host: "a.example", port: 443 },
  { id: "c", name: "Charlie", protocol: "vless", host: "c.example", port: 443 },
];

describe("sortNodes", () => {
  it("sorts by name asc/desc", () => {
    expect(sortNodes(nodes, "name-asc", new Map(), {}).map((n) => n.id)).toEqual(["a", "b", "c"]);
    expect(sortNodes(nodes, "name-desc", new Map(), {}).map((n) => n.id)).toEqual(["c", "b", "a"]);
  });

  it("sorts by ping (ok first, then latency)", () => {
    const pingMap = new Map<string, PingResult>([
      ["a", { nodeId: "a", host: "a", port: 443, latencyMs: 200, ok: true }],
      ["b", { nodeId: "b", host: "b", port: 443, latencyMs: 50, ok: true }],
      ["c", { nodeId: "c", host: "c", port: 443, latencyMs: 0, ok: false, error: "timeout" }],
    ]);
    expect(sortNodes(nodes, "ping", pingMap, {}).map((n) => n.id)).toEqual(["b", "a", "c"]);
  });

  it("sorts by frequent selects", () => {
    const counts = { a: 1, b: 5, c: 5 };
    expect(sortNodes(nodes, "frequent", new Map(), counts).map((n) => n.id)).toEqual([
      "b",
      "c",
      "a",
    ]);
  });
});

describe("orderNodes", () => {
  it("keeps selected first", () => {
    const ordered = orderNodes(nodes, "c", "name-asc", new Map(), {});
    expect(ordered.map((n) => n.id)).toEqual(["c", "a", "b"]);
  });
});
