/** Способы добавления подписки в UI / API. */
export type SubImportSource = "url" | "text" | "uri" | "file" | "ovpn";

export type AddSubscriptionInput = {
  name: string;
  source: SubImportSource;
  url?: string;
  content?: string;
  fileName?: string;
  username?: string;
  password?: string;
  autoRefresh: boolean;
  refreshIntervalMinutes: number;
};

/** .ovpn: клиентский профиль с remote / inline PEM. */
export function looksLikeOvpn(content: string, fileName?: string): boolean {
  const name = (fileName ?? "").toLowerCase();
  if (name.endsWith(".ovpn")) return true;
  const text = content.trim();
  if (!text) return false;
  const low = text.toLowerCase();
  if (low.includes("remote ") && (low.includes("<ca>") || low.includes("\nca "))) {
    return true;
  }
  return low.includes("client") && low.includes("remote ") && low.includes("proto ");
}

/** Тело POST /api/subscriptions (file → text|ovpn на сервере). */
export function buildAddSubscriptionBody(input: AddSubscriptionInput): Record<string, unknown> {
  const name = input.name.trim();
  let source: string = input.source;
  if (source === "file") {
    source = looksLikeOvpn(input.content ?? "", input.fileName) ? "ovpn" : "text";
  }
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
  const body: Record<string, unknown> = {
    ...base,
    content: (input.content ?? "").trim(),
  };
  if (source === "ovpn") {
    const user = (input.username ?? "").trim();
    const pass = input.password ?? "";
    if (user) body.username = user;
    if (pass) body.password = pass;
  }
  return body;
}

export function isRemoteSubscriptionUrl(url?: string): boolean {
  const u = (url ?? "").trim();
  return u.startsWith("http://") || u.startsWith("https://");
}

/** Нужны ли поля логин/пароль для выбранного файла/текста. */
export function ovpnNeedsAuth(content: string): boolean {
  const low = content.toLowerCase();
  return /\bauth-user-pass\b/.test(low);
}
