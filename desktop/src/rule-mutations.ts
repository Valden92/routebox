import { api, type RuleAction } from "./api";

/** Older daemons interpret unknown actions as direct; never send a deny to them. */
export async function saveRule(
  endpoint: string,
  method: "POST" | "PUT",
  body: { pattern?: string; path?: RuleAction; enabled?: boolean },
  request: typeof api = api,
) {
  if (body.path === "block") {
    const version = await request<{ apiVersion?: number }>("/api/version");
    if ((version.apiVersion ?? 0) < 8) {
      throw new Error("Для запрета нужен новый демон. Перезапустите приложение через make dev");
    }
  }
  return request(endpoint, { method, body: JSON.stringify(body) });
}
