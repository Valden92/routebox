/** Способы добавления подписки в UI / API. */
export type SubImportSource = "url" | "text" | "uri" | "file";

export type AddSubscriptionInput = {
  name: string;
  source: SubImportSource;
  url?: string;
  content?: string;
  autoRefresh: boolean;
  refreshIntervalMinutes: number;
};

/** Тело POST /api/subscriptions (file → source text на сервере). */
export function buildAddSubscriptionBody(input: AddSubscriptionInput): Record<string, unknown> {
  const name = input.name.trim();
  const source = input.source === "file" ? "text" : input.source;
  const base: Record<string, unknown> = {
    name,
    source,
    autoRefresh: false,
    refreshIntervalMinutes: 0,
  };
  if (source === "url") {
    return {
      ...base,
      url: (input.url ?? "").trim(),
      autoRefresh: input.autoRefresh,
      refreshIntervalMinutes: input.autoRefresh ? input.refreshIntervalMinutes : 0,
    };
  }
  return {
    ...base,
    content: (input.content ?? "").trim(),
  };
}

export function isRemoteSubscriptionUrl(url?: string): boolean {
  const u = (url ?? "").trim();
  return u.startsWith("http://") || u.startsWith("https://");
}
